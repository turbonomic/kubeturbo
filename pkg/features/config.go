package features

type FeatureGatesConfig struct {
	Name          string            `json:"name"`
	Configuration ConfigurationMode `json:"configuration"`
}

type ConfigurationMode string

const (
	ConfigurationEnabled  ConfigurationMode = "Enabled"
	ConfigurationDisabled ConfigurationMode = "Disabled"
)

type FeatureGates struct {
	Features []FeatureGatesConfig `json:"features,omitempty"`
}
