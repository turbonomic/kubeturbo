package kubeturbo

import (
	"strings"
	"testing"

	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
)

func TestParseK8sTAPServiceSpecWithNeitherTargetNameNorTargetType(t *testing.T) {
	configPath := "../test/config/turbo-config"

	_, err := ParseK8sTAPServiceSpec(configPath)
	if err == nil {
		t.Fatalf("Error while parsing the spec file %s: "+
			"spec with neither targetName nor targetType is not allowed", configPath)
	}
}

func TestParseK8sTAPServiceSpecWithTargetType(t *testing.T) {
	configPath := "../test/config/turbo-config-with-target-type"

	got, err := ParseK8sTAPServiceSpec(configPath)

	if err != nil {
		t.Fatalf("Error while parsing the spec file %s: %v", configPath, err)
	}

	// Check target config
	checkStartWith(got.TargetType, "Kubernetes-", t)
	checkStartWith(got.ProbeCategory, "Cloud Native", t)
	check(got.TargetIdentifier, "", t)

	// Check comm config
	check(got.TurboServer, "https://127.1.1.1:9444", t)
	check(got.RestAPIConfig.OpsManagerUsername, defaultUsername, t)
	check(got.RestAPIConfig.OpsManagerPassword, defaultPassword, t)
}

func TestParseK8sTAPServiceSpecWithTargetName(t *testing.T) {
	configPath := "../test/config/turbo-config-with-target-name"

	got, err := ParseK8sTAPServiceSpec(configPath)

	if err != nil {
		t.Fatalf("Error while parsing the spec file %s: %v", configPath, err)
	}

	// Check target config
	checkStartWith(got.ProbeCategory, "Cloud Native", t)
	// The target name should be the one from the config file
	check(got.TargetType, "Kubernetes-cluster-foo", t)
	check(got.TargetIdentifier, "Kubernetes-cluster-foo", t)
}

func TestParseK8sTAPServiceSpecWithTargetNameAndTargetType(t *testing.T) {
	configPath := "../test/config/turbo-config-with-target-name-and-target-type"

	got, err := ParseK8sTAPServiceSpec(configPath)

	if err != nil {
		t.Fatalf("Error while parsing the spec file %s: %v", configPath, err)
	}

	// Check target config
	checkStartWith(got.ProbeCategory, "Cloud Native", t)
	// The target name should be the one from the config file
	check(got.TargetType, "Kubernetes-Openshift", t)
	check(got.TargetIdentifier, "Kubernetes-cluster-foo", t)
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
