package discovery

import "errors"

const (
	// When user doesn't specify username and password, the default username and password will be used.
	defaultUsername = "defaultUser"
	defaultPassword = "defaultPassword"
)

type K8sTargetConfig struct {
	ProbeCategory    string `json:"probeCategory,omitempty"`
	TargetType       string `json:"targetType,omitempty"`
	TargetIdentifier string `json:"address,omitempty"`
	TargetUsername   string `json:"username,omitempty"`
	TargetPassword   string `json:"password,omitempty"`
}

func NewK8sTargetConfig(probeCategory, targetType, id, username, password string) *K8sTargetConfig {
	return &K8sTargetConfig{
		ProbeCategory:    probeCategory,
		TargetType:       targetType,
		TargetIdentifier: id,
		TargetUsername:   username,
		TargetPassword:   password,
	}
}

func (config *K8sTargetConfig) ValidateK8sTargetConfig() error {
	if config.ProbeCategory == "" || config.TargetType == "" {
		return errors.New("Either probeCategory or targetType is not provided.")
	}
	if config.TargetIdentifier == "" {
		return errors.New("targetIdenfifier is not provided.")
	}
	if config.TargetUsername == "" {
		config.TargetUsername = defaultUsername
	}
	if config.TargetPassword == "" {
		config.TargetPassword = defaultPassword
	}
	return nil
}
