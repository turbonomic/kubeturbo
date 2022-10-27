package gitops

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang/glog"
	gitlab "github.com/xanzy/go-gitlab"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	typedClient "k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/util"
)

type GitLabManager struct {
	gitConfig    GitConfig
	typedClient  *typedClient.Clientset
	dynClient    dynamic.Interface
	obj          *unstructured.Unstructured
	managerApp   *repository.K8sApp
	k8sClusterId string
}

func NewGitLabManager(gitConfig GitConfig, typedClient *typedClient.Clientset, dynClient dynamic.Interface,
	obj *unstructured.Unstructured, managerApp *repository.K8sApp, clusterId string) GitopsManager {
	return &GitHubManager{
		gitConfig:    gitConfig,
		typedClient:  typedClient,
		dynClient:    dynClient,
		obj:          obj,
		managerApp:   managerApp,
		k8sClusterId: clusterId,
	}
}

func (r *GitLabManager) Update(replicas int64, podSpec map[string]interface{}) (interface{}, error) {
	token, err := r.getAuthTokenFromSecret()
	if err != nil {
		return nil, err
	}

	path, repo, revision, err := r.getFieldsFromManagerApp()
	if err != nil {
		return nil, err
	}

	// Parse the URL and ensure there are no errors.
	url, err := url.Parse(repo)
	if err != nil {
		return nil, fmt.Errorf("repo url: %s in manager app of workload controller: %s seems invalid: %v", repo, r, err)
	}

	if !strings.HasPrefix(url.Host, "git.") {
		return nil, fmt.Errorf("repo host: %s unsupported as gitlab remote for workload controller: %s", url.Host, r)
	}

	pathParts := strings.Split(url.Path, "/")
	// We get three parts for a git repo like below:
	// For github.com/irfanurrehman/kubeturbo
	// Path == /irfanurrehman/kubeturbo
	// pathParts[0] = ""
	// pathParts[1] = "irfanurrehman"
	// pathParts[2] = "kubeturbo"
	if len(pathParts) != 3 {
		return nil, fmt.Errorf("source url: %s in manager app of workload controller: %s is not valid. "+
			"It should have 2 sections in path", repo, r)
	}

	baseBranch := revision
	ctx := context.Background()
	client, err := gitlab.NewClient(token)
	if err != nil {
		return nil, err
	}
	handler := &GitLabHandler{
		ctx:         ctx,
		client:      client,
		user:        pathParts[1],
		repo:        strings.TrimSuffix(pathParts[2], ".git"),
		baseBranch:  baseBranch,
		path:        path,
		commitUser:  r.gitConfig.GitUsername,
		commitEmail: r.gitConfig.GitEmail,
		commitMode:  r.gitConfig.CommitMode,
	}

	if baseBranch == "HEAD" {
		baseBranch, err = handler.getHeadBranch()
		if err != nil || baseBranch == "" {
			return nil, fmt.Errorf("problem retrieving HEAD branch for %s. Found %s with error: %v",
				repo, baseBranch, err)
		}
		handler.baseBranch = baseBranch
	}

	glog.Infof("Updating the source of truth at: %s and path: %s.", url.Path, path)
	return handler.updateRemote(r.obj, replicas, podSpec, r.k8sClusterId)
}

func (r *GitLabManager) WaitForActionCompletion(completionData interface{}) error {
	if r.gitConfig.CommitMode == CommitModeDirect || completionData == nil {
		// Commit is already created, no wait needed further
		return nil
	}

	var waitData gitWaitData
	switch data := completionData.(type) {
	case gitWaitData:
		waitData = data
	default:
		return fmt.Errorf("wrong type of completion data received in"+
			"githubManager waitforActionCompletion: expected: [gitWaitData], got: [%t]", data)
	}

	// We currently wait for the PR to be merged with a big timeout (1 week), which is
	// although unrealistic for an action to be completed but still big enough to not
	// time out the action prematurely in kubeturbo.
	// TODO: At some point we will need a better strategy to handle long running actions.
	return wait.PollImmediate(20*time.Second, 24*7*time.Hour, func() (bool, error) {
		handler := waitData.handler
		// Find out the status of the PR via the PR details and the isMerge API
		pr, _, err := handler.client.PullRequests.Get(handler.ctx, handler.user, handler.repo, waitData.prNum)
		if err != nil || pr == nil {
			// We do not return error and exit here to ensure we keep retrying
			// in case of transient network issues
			glog.Errorf("Waiting on action completion for PR number #%d. "+
				"Error accessing github API: %v", waitData.prNum, err)
			return false, nil
		}
		switch pr.GetState() {
		case "open":
			glog.V(4).Infof("Waiting on action completion. PR number #%d is still open.", waitData.prNum)
			return false, nil
		case "closed":
			isMerged, _, err := handler.client.PullRequests.IsMerged(handler.ctx, handler.user, handler.repo, waitData.prNum)
			if err != nil {
				glog.Errorf("Waiting on action completion for PR number #%d. "+
					"Error accessing github API: %v.", waitData.prNum, err)
				return false, nil
			}
			if isMerged {
				glog.V(4).Infof("Found PR number #%d merged. Send action success to server", waitData.prNum)
				return true, nil
			}
			return false, util.NewSkipRetryError(fmt.Sprintf("the PR #%d was closed without merging", waitData.prNum))
		default:
			// We will retry in case of invalid PR state received
			// TODO: a really rare scenario, but if we hit it implement a timeout expiry
			// for this case
			glog.Errorf("Waiting on action completion for PR number #%d. "+
				"Received invalid PR state (%s) in API response.", waitData.prNum, pr.GetState())
		}
		// Retry
		return false, nil
	})
}

func (r *GitLabManager) getAuthTokenFromSecret() (string, error) {
	name := r.gitConfig.GitSecretName
	namespace := r.gitConfig.GitSecretNamespace
	if namespace == "" {
		// try getting the namespace env var set as downstream API value in deployment spec
		namespace = os.Getenv(KubeturboNamespaceEnv)
	}
	if namespace == "" {
		namespace = "default"
	}

	if r.gitConfig.GitSecretName == "" {
		return "", fmt.Errorf("secret name found empty while updating the github repo for workload controller %s. "+
			"It is necessary to get github auth token", r)
	}
	secret, err := r.typedClient.CoreV1().Secrets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	token, exists := secret.Data["token"]
	if !exists {
		return "", fmt.Errorf("wrong data in secret: %s/%s while updating the github repo for workload controller %s. "+
			"Key with name 'token' not found", namespace, name, r)
	}
	return strings.TrimSpace(string(token)), nil
}

func (r *GitLabManager) getFieldsFromManagerApp() (string, string, string, error) {
	if r.managerApp == nil {
		return "", "", "", fmt.Errorf("workload controller not managed by gitops pipeline: %s", r)
	}
	res := schema.GroupVersionResource{
		Group:    util.ArgoCDApplicationGV.Group,
		Version:  util.ArgoCDApplicationGV.Version,
		Resource: util.ApplicationResName,
	}

	app, err := r.dynClient.Resource(res).Namespace(r.managerApp.Namespace).Get(context.TODO(), r.managerApp.Name, metav1.GetOptions{})
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get manager app for workload controller %s: %v", r, err)
	}

	var path, repo, revision string
	found := false
	path, found, err = unstructured.NestedString(app.Object, "spec", "source", "path")
	if err != nil || !found {
		return "", "", "", fmt.Errorf("required field path not found in manager app %s/%s: %v", app.GetNamespace(), app.GetName(), err)
	}
	repo, found, err = unstructured.NestedString(app.Object, "spec", "source", "repoURL")
	if err != nil || !found {
		return "", "", "", fmt.Errorf("required field repoURL not found in manager app %s/%s: %v", app.GetNamespace(), app.GetName(), err)
	}
	revision, found, err = unstructured.NestedString(app.Object, "spec", "source", "targetRevision")
	if err != nil || !found {
		return "", "", "", fmt.Errorf("required field targetRevision not found in manager app %s/%s: %v", app.GetNamespace(), app.GetName(), err)
	}

	return path, repo, revision, nil
}

func (r *GitLabManager) String() string {
	return fmt.Sprintf("%s/%s", r.obj.GetNamespace(), r.obj.GetName())
}

type GitLabHandler struct {
	ctx         context.Context
	client      *gitlab.Client
	user        string
	repo        string
	baseBranch  string
	path        string
	commitUser  string
	commitEmail string
	commitMode  string
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
		if node.Mode != "blob" {
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

func (g *GitLabHandler) updateRemote(res *unstructured.Unstructured, replicas int64,
	podSpec map[string]interface{}, clusterId string) (interface{}, error) {
	resName := res.GetName()
	commitBranchName := g.baseBranch

	configYaml, filePath, err := g.getRemoteFileContent(resName, g.path, commitBranchName)
	if err != nil {
		return nil, err
	}
	needsUpdate, patchedYamlContent, err := PatchYamlContent(configYaml, replicas, podSpec)
	if err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	if !needsUpdate {
		// Its commit mode, we simply log and succeed the action
		glog.Infof("The base branch %s is already aligned to desired state. "+
			"Nothing to commit. Action will be considered successful.", commitBranchName)
		return nil, nil
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
		return nil, fmt.Errorf("error committing new content to branch %s, %v", g.baseBranch, err)
	}

	return nil, nil
}
