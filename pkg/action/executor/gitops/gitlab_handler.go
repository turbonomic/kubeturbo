package gitops

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	gitlab "github.com/xanzy/go-gitlab"
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
)

type GitLabHandler struct {
	GitHandler
	client *gitlab.Client
}

func NewGitLabHandler(gitHandler GitHandler, client *gitlab.Client) GitRemoteHandler {
	return &GitLabHandler{
		GitHandler: gitHandler,
		client:     client,
	}
}

func (g *GitLabHandler) getHeadBranch() (string, error) {
	project, _, err := g.client.Projects.GetProject(g.user+"/"+g.repo, nil)
	if err != nil {
		return "", err
	}

	if project.DefaultBranch == "" {
		return "", fmt.Errorf("error finding the default branch on the remote project %s/%s,", g.user, g.repo)
	}
	return project.DefaultBranch, err
}

func (g *GitLabHandler) ReconcileHeadBranch() error {
	var err error
	baseBranch := g.baseBranch
	if baseBranch == "HEAD" {
		baseBranch, err = g.getHeadBranch()
		if err != nil || baseBranch == "" {
			return fmt.Errorf("problem retrieving HEAD branch for %s. Found %s with error: %v",
				g.repo, baseBranch, err)
		}
		g.baseBranch = baseBranch
	}
	return nil
}

func (g *GitLabHandler) projectID() string {
	return g.user + "/" + g.repo
}

func (g *GitLabHandler) getRemoteFileContent(resName, path, branchRef string) (string, string, error) {
	recursive := true
	options := gitlab.ListTreeOptions{
		Path:      &path,
		Recursive: &recursive,
		Ref:       &branchRef,
	}

	treeNodes, _, err := g.client.Repositories.ListTree(g.projectID(), &options)
	if err != nil {
		return "", "", err
	}

	for _, node := range treeNodes {
		if node.Type != "blob" {
			continue
		}
		// We look for only those tree nodes which are marked as type blob
		fileData, err := g.getFileContent(resName, node.Path, branchRef)
		if err != nil {
			glog.Warningf("Error getting remote file content for %s: %s", node.Path, err)
			// We continue to look for other files hoping this was a transient error
			continue
		}
		match, err := DecodeAndMatchName(fileData, resName)
		if err != nil {
			return "", "", err
		}
		if match {
			return fileData, node.Path, nil
		}
	}

	return "", "", fmt.Errorf("file with metadata.name %s not found in remote repo %s,", resName, g.repo)
}

func (g *GitLabHandler) getFileContent(resName, filePath, branchRef string) (string, error) {
	options := gitlab.GetFileOptions{
		Ref: &branchRef,
	}

	file, _, err := g.client.RepositoryFiles.GetFile(g.projectID(), filePath, &options)
	if err != nil || file == nil {
		return "", err
	}

	if file.Encoding == "base64" {
		c, err := base64.StdEncoding.DecodeString(file.Content)
		return string(c), err
	}

	return file.Content, nil
}

func (g *GitLabHandler) UpdateRemote(res *unstructured.Unstructured, replicas int64,
	podSpec map[string]interface{}, clusterId string) (WaitForCompletionFn, interface{}, error) {
	resName := res.GetName()
	newBranchName := ""
	contentBranchName := g.baseBranch
	var mr *gitlab.MergeRequest
	var err error
	resNamespace := res.GetNamespace()
	if g.commitMode == CommitModeRequest {
		mr, err = g.checkMrExists(resName, resNamespace, clusterId)
		if err != nil {
			return nil, nil, err
		}
		if mr != nil {
			glog.V(2).Infof("Found an existing MR #%d while executing action for "+
				clusterId+"/"+resNamespace+"/"+resName, mr.IID)
			contentBranchName = mr.SourceBranch
		} else {
			// gitlab createCommit api provides an option of creating the branch
			// with the commit, so additional create_branch request is not needed
			newBranchName = g.baseBranch + "-" + strconv.FormatInt(time.Now().UnixNano(), 32)
			glog.V(3).Infof("A new branch %s will be created with a new commit.", newBranchName)
		}
	}

	configYaml, filePath, err := g.getRemoteFileContent(resName, g.path, contentBranchName)
	if err != nil {
		return nil, nil, err
	}
	needsUpdate, patchedYamlContent, err := PatchYamlContent(configYaml, replicas, podSpec)
	if err != nil {
		return nil, nil, err
	}
	if !needsUpdate {
		if mr != nil {
			// We have an existing MR with this action
			// We should now wait on the existing MR to be merged
			glog.Infof("The open MR #%d content is already aligned to desired state. "+
				"Will now wait for the MR to be merged to complete the action.", mr.IID)
			return GitLabWaitForCompletionFn, gitLabWaitData{
				handler: g,
				mrNum:   mr.IID,
			}, nil
		}
		// Its commit mode, we simply log and succeed the action
		glog.Infof("The content branch %s is already aligned to desired state. "+
			"Nothing to commit. Action will be considered successful.", contentBranchName)
		return nil, nil, nil
	}

	updateAction := gitlab.FileUpdate
	commitAction := gitlab.CommitActionOptions{
		Action:   &updateAction,
		FilePath: &filePath,
		Content:  gitlab.String(base64.StdEncoding.EncodeToString(patchedYamlContent)),
		Encoding: gitlab.String("base64"),
	}
	commitActions := []*gitlab.CommitActionOptions{
		&commitAction,
	}

	commitOpts := gitlab.CreateCommitOptions{
		CommitMessage: gitlab.String("Turbonomic Action: update yaml file " + filePath),
		AuthorName:    gitlab.String(g.commitUser),
		AuthorEmail:   gitlab.String(g.commitEmail),
		Actions:       commitActions,
	}
	if g.commitMode == CommitModeRequest {
		if mr != nil {
			commitOpts.Branch = gitlab.String(mr.SourceBranch)
		} else {
			// Documentation for start_branch is a little confusing
			// A reasonable explaination is given at below issue comment
			// https://gitlab.com/gitlab-org/gitlab/-/issues/342200#note_810898339
			commitOpts.Branch = gitlab.String(newBranchName)
			commitOpts.StartBranch = gitlab.String(g.baseBranch)
		}
	} else {
		// For direct commit mode, we simply update the base branch to commit on to
		commitOpts.Branch = gitlab.String(g.baseBranch)
	}

	_, _, err = g.client.Commits.CreateCommit(g.projectID(), &commitOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("error committing new content to branch %s, %v", g.baseBranch, err)
	}

	if g.commitMode == CommitModeRequest {
		if mr == nil {
			mr, err = g.newMR(resName, resNamespace, clusterId, newBranchName)
			if err != nil {
				return nil, nil, err
			}
			glog.Infof("MR #%d created. "+
				"Will now wait for the MR to be merged to complete the action.", mr.IID)
		} else {
			glog.Infof("MR #%d exists. "+
				"Will now wait for the MR to be merged to complete the action.", mr.IID)
		}
		return GitLabWaitForCompletionFn, gitLabWaitData{
			handler: g,
			mrNum:   mr.IID,
		}, nil
	}

	return nil, nil, nil
}

func (g *GitLabHandler) checkMrExists(resName, resNamespace, clusterId string) (*gitlab.MergeRequest, error) {
	options := &gitlab.ListProjectMergeRequestsOptions{
		State:          gitlab.String("opened"),
		TargetBranch:   gitlab.String(g.baseBranch),
		AuthorUsername: gitlab.String(g.commitUser),
	}
	mrList, _, err := g.client.MergeRequests.ListProjectMergeRequests(g.projectID(), options)
	if err != nil {
		return nil, err
	}

	for _, mr := range mrList {
		if strings.HasSuffix(mr.Title, clusterId+"/"+resNamespace+"/"+resName) {
			return mr, nil
		}
	}
	return nil, nil
}

func (g *GitLabHandler) newMR(resName, resNamespace, clusterId string, newBranch string) (*gitlab.MergeRequest, error) {
	mrTitle := fmt.Sprintf("Turbonomic Action: update yaml file %s for resource %s/%s/%s",
		g.path, clusterId, resNamespace, resName)
	mrDescription := "This MR is automatically created via `Turbonomic action execution` \n\n" +
		"This MR intends to update pod template resources or replicas of resource `" + resName + "`"
	options := &gitlab.CreateMergeRequestOptions{
		Title:              &mrTitle,
		SourceBranch:       &newBranch,
		Description:        &mrDescription,
		TargetBranch:       gitlab.String(g.baseBranch),
		RemoveSourceBranch: gitlab.Bool(true),
	}

	mr, _, err := g.client.MergeRequests.CreateMergeRequest(g.projectID(), options)
	if err != nil {
		return nil, err
	}
	return mr, nil
}

func GitLabWaitForCompletionFn(completionData interface{}) error {
	var waitData gitLabWaitData
	switch data := completionData.(type) {
	case gitLabWaitData:
		waitData = data
	default:
		return fmt.Errorf("wrong type of completion data received in"+
			"gitlabManager waitforActionCompletion: expected: [gitLabWaitData], got: [%t]", data)
	}

	// We currently wait for the MR to be merged with a big timeout (1 week), which is
	// although unrealistic for an action to be completed but still big enough to not
	// time out the action prematurely in kubeturbo.
	// TODO: At some point we will need a better strategy to handle long running actions.
	return wait.PollImmediate(20*time.Second, 24*7*time.Hour, func() (bool, error) {
		handler := waitData.handler
		mrNumber := waitData.mrNum
		mr, _, err := handler.client.MergeRequests.GetMergeRequest(handler.projectID(), mrNumber, &gitlab.GetMergeRequestsOptions{})
		if err != nil || mr == nil {
			// We do not return error and exit here to ensure we keep retrying
			// in case of transient network issues
			glog.Errorf("Waiting on action completion for MR number #%d. "+
				"Error accessing gitlab API: %v", mrNumber, err)
			return false, nil
		}
		switch mr.State {
		// state can be opened, closed, merged or locked.
		case "opened":
			glog.V(4).Infof("Waiting on action completion. MR number #%d is still open.", mrNumber)
			return false, nil
		case "merged":
			glog.V(4).Infof("Found MR number #%d merged. Send action success to server", mrNumber)
			return true, nil
		case "closed":
			return false, util.NewSkipRetryError(fmt.Sprintf("the MR #%d was closed without merging", mrNumber))
		default:
			// We will retry in case of invalid/unhandled MR state received
			// This could be gitlabs "locked" state, we keep waiting for such MR
			glog.Errorf("Waiting on action completion for MR number #%d. "+
				"Received unhandled MR state (%s) in API response.", mrNumber, mr.State)
		}
		// Retry
		return false, nil
	})
}
