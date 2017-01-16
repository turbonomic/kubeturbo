package discovery

type K8sTargetConfig struct {
	targetIdentifier string
	username         string
	password         string
}

func NewK8sTargetConfig(id, username, password string) *K8sTargetConfig {
	return &K8sTargetConfig{
		targetIdentifier: id,
		username:         username,
		password:         password,
	}
}
