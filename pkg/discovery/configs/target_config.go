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
	defaultProbeCategory = "Cloud Native"
	defaultTargetType    = "Kubernetes"
)

type K8sTargetConfig struct {
	ProbeCategory    string `json:"probeCategory,omitempty"`
	TargetType       string `json:"targetType,omitempty"`
	TargetIdentifier string `json:"targetName,omitempty"`
	TargetUsername   string `json:"-"`
	TargetPassword   string `json:"-"`

	// TODO: Remove it when backward compatibility is not needed
	TargetAddress string `json:"address,omitempty"`
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
		if config.TargetAddress != "" { // TODO: Remove it when backward compatibility is not needed
			config.TargetIdentifier = config.TargetAddress
		} else {
			return errors.New("targetIdentifier is not provided")
		}
	}

	// Prefix target id (address) with the target type (i.e., "Kubernetes-") to
	// avoid duplicate target id with other types of targets (e.g., aws).
	config.TargetIdentifier = defaultTargetType + "-" + config.TargetIdentifier

	if config.TargetUsername == "" {
		config.TargetUsername = defaultUsername
	}
	if config.TargetPassword == "" {
		config.TargetPassword = defaultPassword
	}
	if config.ProbeCategory == "" {
		config.ProbeCategory = defaultProbeCategory
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
