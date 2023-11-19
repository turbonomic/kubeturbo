package gitops

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/golang/glog"
	"github.com/google/go-github/v42/github"
	gitlab "github.com/xanzy/go-gitlab"
	"golang.org/x/oauth2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	typedClient "k8s.io/client-go/kubernetes"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
)

type gitHubWaitData struct {
	handler *GitHubHandler
	prNum   int
}

type gitLabWaitData struct {
	handler *GitLabHandler
	mrNum   int
}

type GitManager struct {
	gitConfig    GitConfig
	typedClient  *typedClient.Clientset
	dynClient    dynamic.Interface
	obj          *unstructured.Unstructured
	managerApp   *repository.K8sApp
	k8sClusterId string
}

func NewGitManager(gitConfig GitConfig, typedClient *typedClient.Clientset, dynClient dynamic.Interface,
	obj *unstructured.Unstructured, managerApp *repository.K8sApp, clusterId string) GitopsManager {
	return &GitManager{
		gitConfig:    gitConfig,
		typedClient:  typedClient,
		dynClient:    dynClient,
		obj:          obj,
		managerApp:   managerApp,
		k8sClusterId: clusterId,
	}
}

func (r *GitManager) Update(replicas int64, podSpec map[string]interface{}) (WaitForCompletionFn, interface{}, error) {
	token, err := r.getAuthTokenFromSecret()
	if err != nil {
		return nil, nil, err
	}

	path, repo, revision, err := r.getFieldsFromManagerApp()
	if err != nil {
		return nil, nil, err
	}

	// Parse the URL and ensure there are no errors.
	url, err := url.Parse(repo)
	if err != nil {
		return nil, nil, fmt.Errorf("repo url: %s in manager app of workload controller: %s seems invalid: %v", repo, r, err)
	}
	pathParts := strings.Split(url.Path, "/")
	// We get three parts for a git repo like below:
	// For https://github.com/irfanurrehman/kubeturbo
	// Path == /irfanurrehman/kubeturbo
	// pathParts[0] = ""
	// pathParts[1] = "irfanurrehman"
	// pathParts[2] = "kubeturbo"
	// Same goes for gitlab url scheme eg:
	// 	https://git.turbonomic.com/irfanurrehman/Turbo-ScenarioAsCode.git
	if len(pathParts) != 3 {
		return nil, nil, fmt.Errorf("source url: %s in manager app of workload controller: %s is not valid. "+
			"It should have 2 sections in path", repo, r)
	}

	var handler GitRemoteHandler
	ctx := context.Background()
	gitHandler := GitHandler{
		ctx:         ctx,
		user:        pathParts[1],
		repo:        strings.TrimSuffix(pathParts[2], ".git"),
		baseBranch:  revision,
		path:        path,
		commitUser:  r.gitConfig.GitUsername,
		commitEmail: r.gitConfig.GitEmail,
		commitMode:  r.gitConfig.CommitMode,
	}

	switch {
	case url.Host == "github.com": // Public GitHub
		handler = NewGitHubHandler(gitHandler, getClient(ctx, token, ""))
	case strings.HasPrefix(url.Host, "github."): // Enterprise GitHub
		handler = NewGitHubHandler(gitHandler, getClient(ctx, token, url.Scheme+"://"+url.Host))
	case strings.HasPrefix(url.Host, "git."):
		client, err := gitlab.NewClient(token, gitlab.WithBaseURL(url.Scheme+"://"+url.Host))
		if err != nil {
			return nil, nil, err
		}
		handler = NewGitLabHandler(gitHandler, client)
	default:
		return nil, nil, fmt.Errorf("repo host: %s unsupported as git remote for workload controller: %s. "+
			"Supported schemes are 'github.com/<org>/<repo>' or 'git.<org>.com/<project>/<repo>'", url.Host, r)
	}

	err = handler.ReconcileHeadBranch()
	if err != nil {
		return nil, nil, err
	}
	glog.Infof("Updating the source of truth at: %s and path: %s.", url.Path, path)
	return handler.UpdateRemote(r.obj, replicas, podSpec, r.k8sClusterId)
}

func (r *GitManager) WaitForActionCompletion(fn WaitForCompletionFn, completionData interface{}) error {
	if r.gitConfig.CommitMode == CommitModeDirect || completionData == nil {
		// Commit is already created, no wait needed further
		return nil
	}

	return fn(completionData)
}

func (r *GitManager) getAuthTokenFromSecret() (string, error) {
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

func (r *GitManager) getFieldsFromManagerApp() (string, string, string, error) {
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

func (r *GitManager) String() string {
	return fmt.Sprintf("%s/%s", r.obj.GetNamespace(), r.obj.GetName())
}

func getClient(ctx context.Context, token, baseUrl string) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	tc := oauth2.NewClient(ctx, ts)
	if baseUrl != "" {
		// We ignore error because we already parse the url before passing it on
		// the only errors expected here are the url parse errors
		client, _ := github.NewEnterpriseClient(baseUrl, baseUrl, tc)
		return client
	}
	return github.NewClient(tc)
}
