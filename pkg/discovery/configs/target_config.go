package configs

import (
	"errors"
	"fmt"
	"hash/fnv"
)

const (
	// When user doesn't specify username and password, the default username and password will be used.
	defaultUsername      = "defaultUser"
	defaultPassword      = "defaultPassword"
	defaultProbeCategory = "CloudNative"
	defaultTargetType    = "Kubernetes"
)

type K8sTargetConfig struct {
	ProbeCategory    string `json:"probeCategory,omitempty"`
	TargetType       string `json:"targetType,omitempty"`
	TargetIdentifier string `json:"targetName,omitempty"`
	TargetUsername   string `json:"-"`
	TargetPassword   string `json:"-"`
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
	if config.TargetIdentifier == "" {
		return errors.New("targetIdentifier is not provided")
	}
	if config.TargetUsername == "" {
		config.TargetUsername = defaultUsername
	}
	if config.TargetPassword == "" {
		config.TargetPassword = defaultPassword
	}
	if config.ProbeCategory == "" {
		// Append a random string to avoid communication issues when there are multiple k8s clusters
		// (as a workaround)
		config.ProbeCategory = appendRandomName(defaultProbeCategory, config.TargetIdentifier)
	}
	if config.TargetType == "" {
		// Append a random string to avoid communication issues when there are multiple k8s clusters
		// (as a workaround)
		config.TargetType = appendRandomName(defaultTargetType, config.TargetIdentifier)
	}
	return nil
}

func appendRandomName(name, append string) string {
	return name + "-" + fmt.Sprint(hash(append))
}

func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}
