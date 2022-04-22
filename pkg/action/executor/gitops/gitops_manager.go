package gitops

type GitopsManager interface {
	Update(replicas int64, podSpec map[string]interface{}) error
}
