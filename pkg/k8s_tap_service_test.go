package kubeturbo

import (
	"strings"
	"testing"

	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
)

func TestParseK8sTAPServiceSpecWithMissingTargetConfig(t *testing.T) {
	defaultTargetName := "target-foo"
	configPath := "../test/config/turbo-config"

	config, err := ParseK8sTAPServiceSpec(configPath, defaultTargetName)
	if err != nil {
		t.Fatalf("Error while parsing the spec file %s: %v", configPath, err)
	}

	// Check target config
	checkStartWith(config.ProbeCategory, "Cloud Native", t)
	check(config.TargetType, "Kubernetes-"+defaultTargetName, t)
	check(config.TargetIdentifier, "Kubernetes-"+defaultTargetName, t)

	// Check comm config
	check(config.TurboServer, "https://127.1.1.1:9444", t)
	check(config.OpsManagerUsername, "foo", t)
	check(config.OpsManagerPassword, "bar", t)
}

func TestParseK8sTAPServiceSpecWithEmptyTargetConfig(t *testing.T) {
	defaultTargetName := "target-foo"
	configPath := "../test/config/turbo-config-with-empty-targetconfig"

	config, err := ParseK8sTAPServiceSpec(configPath, defaultTargetName)
	if err != nil {
		t.Fatalf("Error while parsing the spec file %s: %v", configPath, err)
	}

	// Check target config
	checkStartWith(config.ProbeCategory, "Cloud Native", t)
	check(config.TargetType, "Kubernetes-"+defaultTargetName, t)
	check(config.TargetIdentifier, "Kubernetes-"+defaultTargetName, t)
}

func TestParseK8sTAPServiceSpecWithTargetType(t *testing.T) {
	defaultTargetName := "target-foo"
	configPath := "../test/config/turbo-config-with-target-type"

	config, err := ParseK8sTAPServiceSpec(configPath, defaultTargetName)
	if err != nil {
		t.Fatalf("Error while parsing the spec file %s: %v", configPath, err)
	}

	// Check target config
	checkStartWith(config.ProbeCategory, "Cloud Native", t)
	check(config.TargetType, "Kubernetes-cluster-foo", t)
	check(config.TargetIdentifier, "", t)

	// Check comm config
	check(config.TurboServer, "https://127.1.1.1:9444", t)
	check(config.OpsManagerUsername, defaultUsername, t)
	check(config.OpsManagerPassword, defaultPassword, t)
}

func TestParseK8sTAPServiceSpecWithTargetName(t *testing.T) {
	defaultTargetName := "target-foo"
	configPath := "../test/config/turbo-config-with-target-name"

	config, err := ParseK8sTAPServiceSpec(configPath, defaultTargetName)
	if err != nil {
		t.Fatalf("Error while parsing the spec file %s: %v", configPath, err)
	}

	// Check target config
	checkStartWith(config.ProbeCategory, "Cloud Native", t)
	// The target name should be the one from the config file
	check(config.TargetType, "Kubernetes-cluster-foo", t)
	check(config.TargetIdentifier, "Kubernetes-cluster-foo", t)
	check(config.OpsManagerUsername, "foo", t)
	check(config.OpsManagerPassword, "bar", t)
}

func TestParseK8sTAPServiceSpecWithTargetNameAndTargetType(t *testing.T) {
	defaultTargetName := "target-foo"
	configPath := "../test/config/turbo-config-with-target-name-and-target-type"

	config, err := ParseK8sTAPServiceSpec(configPath, defaultTargetName)

	if err != nil {
		t.Fatalf("Error while parsing the spec file %s: %v", configPath, err)
	}

	// Check target config
	checkStartWith(config.ProbeCategory, "Cloud Native", t)
	// The target name should be the one from the config file
	check(config.TargetType, "Kubernetes-Openshift", t)
	check(config.TargetIdentifier, "Kubernetes-cluster-foo", t)
	check(config.OpsManagerUsername, "foo", t)
	check(config.OpsManagerPassword, "bar", t)

}

func check(got, want string, t *testing.T) {
	if got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}

func checkStartWith(got, want string, t *testing.T) {
	if !strings.HasPrefix(got, want) {
		t.Errorf("got: %v doesn't start with %v", got, want)
	}
}

func TestCreateProbeConfigOrDie(t *testing.T) {
	tests := []struct {
		name                      string
		UseUUID                   bool
		wantStitchingPropertyType stitching.StitchingPropertyType
	}{
		{
			name:                      "test-use-IP",
			UseUUID:                   false,
			wantStitchingPropertyType: stitching.IP,
		},
		{
			name:                      "test-use-UUID",
			UseUUID:                   true,
			wantStitchingPropertyType: stitching.UUID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vmtConfig := NewVMTConfig2().UsingUUIDStitch(tt.UseUUID)
			got := createProbeConfigOrDie(vmtConfig)
			checkProbeConfig(t, got, tt.wantStitchingPropertyType)
		})
	}
}

func checkProbeConfig(t *testing.T, pc *configs.ProbeConfig, stitchingPropertyType stitching.StitchingPropertyType) {

	if pc.StitchingPropertyType != stitchingPropertyType {
		t.Errorf("StitchingPropertyType = %v, want %v", pc.StitchingPropertyType, stitchingPropertyType)
	}
}
