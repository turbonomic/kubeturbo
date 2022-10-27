package gitops

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

type GitopsManager interface {
	Update(replicas int64, podSpec map[string]interface{}) (interface{}, error)
	WaitForActionCompletion(completionData interface{}) error
}
