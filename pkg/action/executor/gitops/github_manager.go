package gitops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/golang/glog"
	"github.com/google/go-github/v42/github"
	"golang.org/x/oauth2"
	k8sapiyaml "k8s.io/apimachinery/pkg/util/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	typedClient "k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/util"
)

const kubeturboNamespaceEnv = "KUBETURBO_NAMESPACE"

type GitConfig struct {
	// Namespace which holds the git secret that stores the git credential token
	GitSecretNamespace string
	// Name of the secret which holds the git credential token
	GitSecretName string
	// Username to be used for git operations on the remote repos
	GitUsername string
	// Email to be used for git operations on the remote repos
	GitEmail string
}

type PatchItem struct {
	Op    string      `json:"op,omitempty"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

type GitHubManager struct {
	gitConfig   GitConfig
	typedClient *typedClient.Clientset
	dynClient   dynamic.Interface
	obj         *unstructured.Unstructured
	managerApp  *repository.K8sApp
}

func NewGitHubManager(gitConfig GitConfig, typedClient *typedClient.Clientset, dynClient dynamic.Interface,
	obj *unstructured.Unstructured, managerApp *repository.K8sApp) GitopsManager {
	return &GitHubManager{
		gitConfig:   gitConfig,
		typedClient: typedClient,
		dynClient:   dynClient,
		obj:         obj,
		managerApp:  managerApp,
	}
}

func (r *GitHubManager) Update(replicas int64, podSpec map[string]interface{}) error {
	token, err := r.getAuthTokenFromSecret()
	if err != nil {
		return err
	}

	path, repo, revision, err := r.getFieldsFromManagerApp()
	if err != nil {
		return err
	}

	// Parse the URL and ensure there are no errors.
	url, err := url.Parse(repo)
	if err != nil {
		return fmt.Errorf("repo url: %s in manager app of workload controller: %s seems invalid: %v", repo, r, err)
	}

	if url.Host != "github.com" {
		return fmt.Errorf("repo host: %s unsupported as gitops remote for workload controller: %s", url.Host, r)
	}

	pathParts := strings.Split(url.Path, "/")
	// We get three parts for a git repo like below:
	// For github.com/irfanurrehman/kubeturbo
	// Path == /irfanurrehman/kubeturbo
	// pathParts[0] = ""
	// pathParts[1] = "irfanurrehman"
	// pathParts[2] = "kubeturbo"
	if len(pathParts) != 3 {
		return fmt.Errorf("source url: %s in manager app of workload controller: %s is not valid. "+
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
	}

	patches := []PatchItem{
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

	glog.Infof("Updating the source of truth at: %s on branch: %s and path: %s.", url.Path, baseBranch, path)
	return handler.updateRemote(r.obj.GetName(), patches)
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
}

func (g *GitHandler) getBranchRef() (*github.Reference, error) {
	baseRef, _, err := g.client.Git.GetRef(g.ctx, g.user, g.repo, "refs/heads/"+g.baseBranch)
	if err != nil {
		return nil, err
	}
	return baseRef, err
}

func (g *GitHandler) getRemoteFileContent(resName, path string) (string, error) {
	opts := github.RepositoryContentGetOptions{
		Ref: g.baseBranch,
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
		fileData, path, err := g.fileContentFromDirContent(resName, dirContent)
		if err != nil {
			return "", err
		}
		g.path = path
		return fileData, err
	}

	return "", fmt.Errorf("file with metadata.name %s not found in remote repo %s,", resName, g.repo)
}

func decodeAndMatchName(fileData, name string) (bool, error) {
	obj := &unstructured.Unstructured{}
	decoder := k8sapiyaml.NewYAMLToJSONDecoder(strings.NewReader(fileData))
	if err := decoder.Decode(obj); err != nil {
		return false, nil
	}

	if obj.GetName() == name {
		return true, nil
	}

	return false, nil
}

func (g *GitHandler) fileContentFromDirContent(resName string, dirContent []*github.RepositoryContent) (string, string, error) {
	opts := github.RepositoryContentGetOptions{
		Ref: g.baseBranch,
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
			return g.fileContentFromDirContent(resName, dirContent)
		}
	}

	return "", "", fmt.Errorf("file with metadata.name %s not found in remote repo %s,", resName, g.repo)
}

func (g *GitHandler) getTree(ref *github.Reference, fileContent []byte) (tree *github.Tree, err error) {
	// Create a tree with what to commit.
	entries := []*github.TreeEntry{
		{
			Path:    github.String(g.path),
			Type:    github.String("blob"),
			Content: github.String(string(fileContent)),
			Mode:    github.String("100644"),
		},
	}

	tree, _, err = g.client.Git.CreateTree(g.ctx, g.user, g.repo, *ref.Object.SHA, entries)
	return tree, err
}

func (r *GitHandler) updateRemote(resName string, patches []PatchItem) error {
	yamlContent, err := r.getRemoteFileContent(resName, r.path)
	if err != nil {
		return err
	}

	patchedYamlContent, err := ApplyPatch([]byte(yamlContent), patches)
	if err != nil {
		return fmt.Errorf("error applying patches to file %s: patches: %v, err: %v", r.path, patches, err)
	}

	branchRef, err := r.getBranchRef()
	if err != nil {
		return err
	}

	tree, err := r.getTree(branchRef, patchedYamlContent)
	if err != nil {
		return fmt.Errorf("error getting branch ref: %s, %v", r.baseBranch, err)
	}

	_, err = r.pushCommit(branchRef, tree)
	if err != nil {
		return fmt.Errorf("error committing new content to branch %s, %v", r.baseBranch, err)
	}
	return nil
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

// TODO: Enhance the support to identify json or yaml content on the fly
func ApplyPatch(yamlBytes []byte, patches []PatchItem) ([]byte, error) {
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
