package gitops

type PatchItem struct {
	Op    string      `json:"op,omitempty"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

type GitopsManager interface {
	Update(replicas int64, podSpec map[string]interface{}) (interface{}, error)
	WaitForActionCompletion(completionData interface{}) error
}
