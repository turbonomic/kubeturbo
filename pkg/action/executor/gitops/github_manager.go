package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/google/go-github/v42/github"
	"golang.org/x/oauth2"

	argodiff "github.com/argoproj/gitops-engine/pkg/diff"
	jsonpatch "github.com/evanphx/json-patch"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	k8sapiyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	typedClient "k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/util"
)

const (
	kubeturboNamespaceEnv = "KUBETURBO_NAMESPACE"
	commitModePR          = "pr"
	commitModeDirect      = "direct"
)

type GitConfig struct {
	// Namespace which holds the git secret that stores the git credential token
	GitSecretNamespace string
	// Name of the secret which holds the git credential token
	GitSecretName string
	// Username to be used for git operations on the remote repos
	GitUsername string
	// Email to be used for git operations on the remote repos
	GitEmail string
	// The mode in which git action should be executed [one of pr/direct]
	CommitMode string
}

type gitWaitData struct {
	handler *GitHandler
	prNum   int
}

type GitHubManager struct {
	gitConfig    GitConfig
	typedClient  *typedClient.Clientset
	dynClient    dynamic.Interface
	obj          *unstructured.Unstructured
	managerApp   *repository.K8sApp
	k8sClusterId string
}

func NewGitHubManager(gitConfig GitConfig, typedClient *typedClient.Clientset, dynClient dynamic.Interface,
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

func (r *GitHubManager) Update(replicas int64, podSpec map[string]interface{}) (interface{}, error) {
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

	if url.Host != "github.com" {
		return nil, fmt.Errorf("repo host: %s unsupported as gitops remote for workload controller: %s", url.Host, r)
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
	// TODO: Figure out how to resolve HEAD while getting the refs from remote repo
	if baseBranch == "HEAD" {
		baseBranch = "master"
	}
	ctx := context.Background()
	handler := &GitHandler{
		ctx:         ctx,
		client:      getClient(ctx, token),
		user:        pathParts[1],
		repo:        strings.TrimSuffix(pathParts[2], ".git"),
		baseBranch:  baseBranch,
		path:        path,
		commitUser:  r.gitConfig.GitUsername,
		commitEmail: r.gitConfig.GitEmail,
		commitMode:  r.gitConfig.CommitMode,
	}

	glog.Infof("Updating the source of truth at: %s and path: %s.", url.Path, path)
	return handler.updateRemote(r.obj, replicas, podSpec, r.k8sClusterId)
}

func (r *GitHubManager) WaitForActionCompletion(completionData interface{}) error {
	if r.gitConfig.CommitMode == commitModeDirect || completionData == nil {
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
		isMerged, _, err := handler.client.PullRequests.IsMerged(handler.ctx, handler.user, handler.repo, waitData.prNum)
		if err != nil {
			return false, err
		}
		if isMerged {
			return true, nil
		}

		// Retry
		return false, nil
	})
}

func (r *GitHubManager) getAuthTokenFromSecret() (string, error) {
	name := r.gitConfig.GitSecretName
	namespace := r.gitConfig.GitSecretNamespace
	if namespace == "" {
		// try getting the namespace env var set as downstream API value in deployment spec
		namespace = os.Getenv(kubeturboNamespaceEnv)
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

func (r *GitHubManager) getFieldsFromManagerApp() (string, string, string, error) {
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

func (r *GitHubManager) String() string {
	return fmt.Sprintf("%s/%s", r.obj.GetNamespace(), r.obj.GetName())
}

func getClient(ctx context.Context, token string) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

type GitHandler struct {
	ctx         context.Context
	client      *github.Client
	user        string
	repo        string
	baseBranch  string
	path        string
	commitUser  string
	commitEmail string
	commitMode  string
}

func (g *GitHandler) getBranchRef(branchName string) (*github.Reference, error) {
	baseRef, _, err := g.client.Git.GetRef(g.ctx, g.user, g.repo, "refs/heads/"+branchName)
	if err != nil {
		return nil, err
	}
	return baseRef, err
}

func (g *GitHandler) getRemoteFileContent(resName, path, branchRef string) (string, error) {
	opts := github.RepositoryContentGetOptions{
		Ref: branchRef,
	}
	fileContent, dirContent, _, err := g.client.Repositories.GetContents(g.ctx, g.user, g.repo, path, &opts)
	if err != nil {
		return "", err
	}
	if fileContent != nil {
		// This means we actually got the path as full file path
		// The GetContents() api exclusively returns fileContent or dirContent.
		return fileContent.GetContent()
	}

	if dirContent != nil {
		fileData, path, err := g.fileContentFromDirContent(resName, branchRef, dirContent)
		if err != nil {
			return "", err
		}
		g.path = path
		return fileData, err
	}

	return "", fmt.Errorf("file with metadata.name %s not found in remote repo %s,", resName, g.repo)
}

func decodeAndMatchName(fileData, name string) (bool, error) {
	obj, err := decodeYaml(fileData)
	if err != nil {
		return false, err
	}
	if obj.GetName() == name {
		return true, nil
	}

	return false, nil
}

func (g *GitHandler) fileContentFromDirContent(resName, branchRef string, dirContent []*github.RepositoryContent) (string, string, error) {
	opts := github.RepositoryContentGetOptions{
		Ref: branchRef,
	}
	for _, repoContent := range dirContent {
		fileContent, dirContent, _, err := g.client.Repositories.GetContents(g.ctx, g.user, g.repo, *repoContent.Path, &opts)
		if err != nil {
			return "", "", err
		}
		if fileContent != nil {
			fileData, err := fileContent.GetContent()
			if err != nil {
				return "", "", err
			}
			match, err := decodeAndMatchName(fileData, resName)
			if err != nil {
				return "", "", err
			}
			if match {
				return fileData, *repoContent.Path, nil
			}
		} else if dirContent != nil {
			return g.fileContentFromDirContent(resName, branchRef, dirContent)
		}
	}

	return "", "", fmt.Errorf("file with metadata.name %s not found in remote repo %s,", resName, g.repo)
}

// createTree works for both creating a new tree and updating an existing one.
func (g *GitHandler) createTree(ref *github.Reference, fileContent []byte) (tree *github.Tree, err error) {
	// Create a tree with what to commit.
	entries := []*github.TreeEntry{
		{
			Path:    github.String(g.path),
			Type:    github.String("blob"),
			Content: github.String(string(fileContent)),
			Mode:    github.String("100644"),
		},
	}

	// If both a tree and a nested path modifying that tree are specified, this endpoint
	// will overwrite the contents of the tree with the new path contents, and create
	// a new tree structure, in effect causing an update tree semantics.

	// ref.Object.SHA
	// The SHA1 of an existing Git tree object which will be used as the base for the new tree.
	// If provided, a new Git tree object will be created from entries in the Git tree object
	// pointed to by base_tree and entries defined in the tree parameter. Entries defined in the
	// tree parameter will overwrite items from base_tree with the same path. If you're creating
	// new changes on a branch, then normally you'd set base_tree to the SHA1 of the Git tree
	// object of the current latest commit on the branch you're working on. If not provided,
	// GitHub will create a new Git tree object from only the entries defined in the tree
	// parameter. If you create a new commit pointing to such a tree, then all files which
	// were a part of the parent commit's tree and were not defined in the tree parameter
	// will be listed as deleted by the new commit.
	tree, _, err = g.client.Git.CreateTree(g.ctx, g.user, g.repo, *ref.Object.SHA, entries)
	return tree, err
}

func (g *GitHandler) updateRemote(res *unstructured.Unstructured, replicas int64,
	podSpec map[string]interface{}, clusterId string) (interface{}, error) {
	var commitBranchName string
	var commitBranchRef *github.Reference
	var pr *github.PullRequest
	var err error
	resName := res.GetName()
	resNamespace := res.GetNamespace()
	if g.commitMode == commitModePR {
		pr, err = g.checkPrExists(resName, resNamespace, clusterId)
		if err != nil {
			return nil, err
		}
		if pr != nil {
			glog.V(2).Infof("Found an existing PR #%d while executing action for "+
				clusterId+"/"+resNamespace+"/"+resName, pr.GetNumber())
			commitBranchName = pr.GetHead().GetRef()
			commitBranchRef, err = g.getBranchRef(commitBranchName)
			if err != nil {
				return nil, fmt.Errorf("error getting branch ref %s, %v", g.baseBranch, err)
			}
		} else {
			// create a new branch and get its ref
			commitBranchName = g.baseBranch + "-" + strconv.FormatInt(time.Now().UnixNano(), 32)
			glog.V(3).Infof("Creating new branch %s.", commitBranchName)
			commitBranchRef, err = g.createNewBranch(commitBranchName)
			if err != nil {
				return nil, fmt.Errorf("error creating new branch %s, %v", commitBranchName, err)
			}
		}
	} else {
		commitBranchName = g.baseBranch
		glog.V(3).Infof("Will use branch %s to directly commit changes on.", commitBranchName)
		commitBranchRef, err = g.getBranchRef(commitBranchName)
		if err != nil {
			return nil, fmt.Errorf("error getting branch ref %s, %v", commitBranchName, err)
		}
	}

	configYaml, err := g.getRemoteFileContent(resName, g.path, commitBranchName)
	if err != nil {
		return nil, err
	}
	needsUpdate, patchedYamlContent, err := patchYamlContent(configYaml, replicas, podSpec)
	if err != nil {
		return nil, err
	}
	if !needsUpdate {
		if pr != nil {
			// We have an existing PR with this action
			// We should now wait on the existing PR to be merged
			glog.Infof("The open pr #%d content is already aligned to desired state. "+
				"Will now wait for the PR to be merged to complete the action.", pr.GetNumber())
			return gitWaitData{
				handler: g,
				prNum:   pr.GetNumber(),
			}, nil
		}
		// Its commit mode, we simply log and succeed the action
		glog.Infof("The base branch %s is already aligned to desired state. "+
			"Nothing to commit. Action will be considered successful.", commitBranchName)
		return nil, nil
	}

	// If pr != nil, commitBranchRef is actually the existing PRs branch, rather
	// then a new branch. Also, if we are here and pr != nil, that means the
	// desired state has drifted from the existing PR. We will push a new commit
	// on the existing PRs branch.
	tree, err := g.createTree(commitBranchRef, patchedYamlContent)
	if err != nil {
		return nil, fmt.Errorf("error getting branch ref: %s, %v", g.baseBranch, err)
	}

	_, err = g.pushCommit(commitBranchRef, tree)
	if err != nil {
		return nil, fmt.Errorf("error committing new content to branch %s, %v", g.baseBranch, err)
	}

	if g.commitMode == commitModePR {
		if pr == nil {
			pr, err = g.newPR(resName, resNamespace, clusterId, commitBranchName)
			if err != nil {
				return nil, err
			}
		}
		glog.Infof("PR #%d created. "+
			"Will now wait for the PR to be merged to complete the action.", pr.GetNumber())
		return gitWaitData{
			handler: g,
			prNum:   pr.GetNumber(),
		}, nil
	}

	return nil, nil
}

// We find the diff between the resource in the source of truth and the liveRes using
// gitops-engine libs which is sort of a wrapper around the k8s libs which implement
// kubectl diff.
//
// This enables us to not fall into the traps of unnecessary diff resulting
// because of differing units but same values and/or extra defaulted fields
// in the original (live) resource. This also takes care of differently ordered keys
// in the specs (effectively comparing the actual resources).
//
// We however still use the json patch mechanism to finally patch the source of truth yaml and
// write it back in the repo.
func patchYamlContent(configYaml string, replicas int64, livePodSpec map[string]interface{}) (bool, []byte, error) {
	configRes, err := decodeYaml(configYaml)
	if err != nil {
		return false, nil, err
	}

	configPodSpec, found, err := unstructured.NestedFieldCopy(configRes.Object, "spec", "template", "spec")
	if err != nil || !found {
		return false, nil, fmt.Errorf("error retrieving podSpec from gitops config resource %s %s: %v",
			configRes.GetKind(), configRes.GetName(), err)
	}
	configPodReplicas, found, err := unstructured.NestedFieldCopy(configRes.Object, "spec", "replicas")
	if err != nil || !found {
		return false, nil, fmt.Errorf("error retrieving replicas from gitops config resource %s %s: %v",
			configRes.GetKind(), configRes.GetName(), err)
	}

	// use copies to ensure our podspecs do not change, yet
	configPod := podWithSpec(configPodSpec)
	livePod := podWithSpec(livePodSpec).DeepCopy()

	// We compare the podSpecs by setting them into a pod/v1 resource because a gvk is expected
	// by the diff lib for comparison to correctly decipher the k8s resource defaulted fields.
	// We could as well have compared the live resource (eg the deployement from which the pod
	// spec is coming) but that results in unwanted diffs because of additional labels or
	// annotations sometimes set locally on the live resource. We should ignore them.
	diffResult, err := argodiff.Diff(configPod, livePod, argodiff.WithNormalizer(argodiff.GetNoopNormalizer()))
	if err != nil {
		return false, nil, err
	}

	modified := diffResult.Modified || configPodReplicas.(int64) != replicas
	var patchedConfigYaml []byte
	if modified {
		// We also remove all the unwanted additional defaulted fields from the live spec
		// so as not to update that back into the source of truth yaml
		livePodSpec = removeMapFields(configPodSpec.(map[string]interface{}), livePodSpec)
		patchedConfigYaml, err = applyPatch([]byte(configYaml), getPatchItems(replicas, livePodSpec))
		if err != nil {
			return false, nil, err
		}
	}

	return modified, patchedConfigYaml, nil
}

func podWithSpec(podSpec interface{}) *unstructured.Unstructured {
	pod := &unstructured.Unstructured{}
	pod.Object = map[string]interface{}{
		"spec": podSpec,
	}
	pod.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Kind:    "Pod",
		Version: "v1",
	})

	return pod
}

func (g *GitHandler) pushCommit(ref *github.Reference, tree *github.Tree) (*github.Reference, error) {
	// Get the parent commit to attach the commit to.
	parent, _, err := g.client.Repositories.GetCommit(g.ctx, g.user, g.repo, *ref.Object.SHA, nil)
	if err != nil {
		return nil, err
	}
	// This is not always populated, but is needed.
	parent.Commit.SHA = parent.SHA

	// Create the commit using the tree.
	date := time.Now()
	authorName := g.commitUser
	authorEmail := g.commitEmail
	commitMsg := "Turbonomic Action: update yaml file " + g.path
	author := &github.CommitAuthor{Date: &date, Name: &authorName, Email: &authorEmail}
	commit := &github.Commit{Author: author, Message: &commitMsg, Tree: tree, Parents: []*github.Commit{parent.Commit}}
	newCommit, _, err := g.client.Git.CreateCommit(g.ctx, g.user, g.repo, commit)
	if err != nil {
		return nil, err
	}

	ref.Object.SHA = newCommit.SHA
	newRef, _, err := g.client.Git.UpdateRef(g.ctx, g.user, g.repo, ref, false)
	return newRef, err
}

func (g *GitHandler) createNewBranch(newBranch string) (ref *github.Reference, err error) {
	var baseRef *github.Reference
	if baseRef, _, err = g.client.Git.GetRef(g.ctx, g.user, g.repo, "refs/heads/"+g.baseBranch); err != nil {
		return nil, err
	}
	newRef := &github.Reference{Ref: github.String("refs/heads/" + newBranch), Object: &github.GitObject{SHA: baseRef.Object.SHA}}
	ref, _, err = g.client.Git.CreateRef(g.ctx, g.user, g.repo, newRef)
	return ref, err
}

func (g *GitHandler) newPR(resName, resNamespace, clusterId string, newBranch string) (*github.PullRequest, error) {
	prTitle := fmt.Sprintf("Turbonomic Action: update yaml file %s for resource %s/%s/%s",
		g.path, clusterId, resNamespace, resName)
	prDescription := "This PR is automatically created via `Turbonomic action execution` \n\n" +
		"This PR intends to update pod template resources or replicas of resource `" + resName + "`"
	baseBranch := g.baseBranch

	newPR := &github.NewPullRequest{
		Title:               &prTitle,
		Head:                &newBranch, // Head may need user:ref_name
		Base:                &baseBranch,
		Body:                &prDescription,
		MaintainerCanModify: github.Bool(true),
	}

	pr, _, err := g.client.PullRequests.Create(g.ctx, g.user, g.repo, newPR)
	if err != nil {
		return nil, err
	}
	return pr, nil
}

func (g *GitHandler) checkPrExists(resName, resNamespace, clusterId string) (*github.PullRequest, error) {
	options := &github.PullRequestListOptions{
		Base: g.baseBranch,
		Head: g.commitUser, // ex head=turbonomic or head=turbonomic: or head=turbonomic:<branch-name>
	}
	prList, _, err := g.client.PullRequests.List(g.ctx, g.user, g.repo, options)
	if err != nil {
		return nil, err
	}

	for _, pr := range prList {
		if pr.Title != nil &&
			strings.HasSuffix(*pr.Title, clusterId+"/"+resNamespace+"/"+resName) &&
			pr.Number != nil {
			return pr, nil
		}
	}
	return nil, nil
}

func decodeYaml(yaml string) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	decoder := k8sapiyaml.NewYAMLToJSONDecoder(strings.NewReader(yaml))
	err := decoder.Decode(obj)
	if err != nil {
		return nil, err
	}

	return obj, nil
}

// copied from https://github.com/argoproj/gitops-engine/blob/v0.7.0/pkg/utils/json/json.go
// but updated to remove all differing fields from live res unlike the original code
// RemoveMapFields remove all non-existent fields in the live that don't exist in the config
func removeMapFields(config, live map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{}
	for k, v1 := range config {
		v2, ok := live[k]
		if !ok {
			continue
		}
		if v2 != nil {
			v2 = removeFields(v1, v2)
		}
		result[k] = v2
	}
	return result
}

func removeFields(config, live interface{}) interface{} {
	if config == nil {
		return nil
	}
	switch c := config.(type) {
	case map[string]interface{}:
		l, ok := live.(map[string]interface{})
		if ok {
			return removeMapFields(c, l)
		} else {
			return live
		}
	case []interface{}:
		l, ok := live.([]interface{})
		if ok {
			return removeListFields(c, l)
		} else {
			return live
		}
	default:
		return live
	}
}

func removeListFields(config, live []interface{}) []interface{} {
	result := make([]interface{}, 0, len(live))
	for i, v2 := range live {
		if len(config) > i {
			if v2 != nil {
				v2 = removeFields(config[i], v2)
			}
			result = append(result, v2)
		} else {
			// skip/remove additional list items which exist in live but not in config
			// this assumes that the lists will be ordered
		}
	}
	return result
}

func getPatchItems(replicas int64, podSpec map[string]interface{}) []PatchItem {
	return []PatchItem{
		{
			Op:    "replace",
			Path:  "/spec/replicas",
			Value: replicas,
		},
		{
			Op:   "replace",
			Path: "/spec/template/spec",
			// TODO: update only specific fields of each container in the pod
			// rather then the whole pod spec
			Value: podSpec,
		},
	}
}

func applyPatch(yamlBytes []byte, patches []PatchItem) ([]byte, error) {
	jsonPatchBytes, err := json.Marshal(patches)
	if err != nil {
		return nil, err
	}

	patch, err := jsonpatch.DecodePatch(jsonPatchBytes)
	if err != nil {
		return nil, err
	}

	jsonBytes, err := yaml.YAMLToJSON(yamlBytes)
	if err != nil {
		return nil, err
	}

	patchedJsonBytes, err := patch.Apply(jsonBytes)
	if err != nil {
		return nil, err
	}

	return yaml.JSONToYAML(patchedJsonBytes)
}
