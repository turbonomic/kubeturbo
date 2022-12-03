package gitops

import (
	"encoding/base64"
	"fmt"

	"github.com/golang/glog"
	gitlab "github.com/xanzy/go-gitlab"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	commitBranchName := g.baseBranch

	configYaml, filePath, err := g.getRemoteFileContent(resName, g.path, commitBranchName)
	if err != nil {
		return nil, nil, err
	}
	needsUpdate, patchedYamlContent, err := PatchYamlContent(configYaml, replicas, podSpec)
	if err != nil {
		return nil, nil, err
	}
	if err != nil {
		return nil, nil, err
	}
	if !needsUpdate {
		// Its commit mode, we simply log and succeed the action
		glog.Infof("The base branch %s is already aligned to desired state. "+
			"Nothing to commit. Action will be considered successful.", commitBranchName)
		return nil, nil, nil
	}

	updateAction := gitlab.FileUpdate
	content := base64.StdEncoding.EncodeToString(patchedYamlContent)
	encoding := "base64"
	commitAction := gitlab.CommitActionOptions{
		Action:   &updateAction,
		FilePath: &filePath,
		Content:  &content,
		Encoding: &encoding,
	}
	commitActions := []*gitlab.CommitActionOptions{
		&commitAction,
	}

	authorName := g.commitUser
	authorEmail := g.commitEmail
	commitMsg := "Turbonomic Action: update yaml file " + filePath
	commitOpts := gitlab.CreateCommitOptions{
		Branch:        &commitBranchName,
		CommitMessage: &commitMsg,
		AuthorName:    &authorName,
		AuthorEmail:   &authorEmail,
		Actions:       commitActions,
	}

	_, _, err = g.client.Commits.CreateCommit(g.projectID(), &commitOpts)
	if err != nil {
		return nil, nil, fmt.Errorf("error committing new content to branch %s, %v", g.baseBranch, err)
	}

	return nil, nil, nil
}
