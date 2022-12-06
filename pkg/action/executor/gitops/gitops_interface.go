package gitops

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

type PatchItem struct {
	Op    string      `json:"op,omitempty"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

type GitHandler struct {
	ctx         context.Context
	user        string
	repo        string
	baseBranch  string
	path        string
	commitUser  string
	commitEmail string
	commitMode  string
}

type WaitForCompletionFn func(completionData interface{}) error

type GitopsManager interface {
	Update(replicas int64, podSpec map[string]interface{}) (WaitForCompletionFn, interface{}, error)
	WaitForActionCompletion(fn WaitForCompletionFn, completionData interface{}) error
}

type GitRemoteHandler interface {
	ReconcileHeadBranch() error
	UpdateRemote(res *unstructured.Unstructured, replicas int64,
		podSpec map[string]interface{}, clusterId string) (WaitForCompletionFn, interface{}, error)
}
