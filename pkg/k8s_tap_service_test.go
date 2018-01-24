package kubeturbo

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"strings"
	"testing"
)

func TestParseK8sTAPServiceSpec(t *testing.T) {
	defaultTargetName := "target-foo"
	configPath := "../test/config/turbo-config"

	got, err := ParseK8sTAPServiceSpec(configPath, defaultTargetName)

	if err != nil {
		t.Fatalf("Error while parsing the spec file %s: %v", configPath, err)
	}

	// Check target config
	checkStartWith(got.TargetType, "Kubernetes-", t)
	checkStartWith(got.ProbeCategory, "CloudNative-", t)
	check(got.TargetIdentifier, defaultTargetName, t)
	check(got.TargetPassword, "defaultPassword", t)
	check(got.TargetUsername, "defaultUser", t)

	// Check comm config
	check(got.TurboServer, "https://127.1.1.1:9444", t)
	check(got.RestAPIConfig.OpsManagerUsername, "foo", t)
	check(got.RestAPIConfig.OpsManagerPassword, "bar", t)
}

func TestParseK8sTAPServiceSpecWithTargetConfig(t *testing.T) {
	defaultTargetName := "target-foo"
	configPath := "../test/config/turbo-config-with-target-name"

	got, err := ParseK8sTAPServiceSpec(configPath, defaultTargetName)

	if err != nil {
		t.Fatalf("Error while parsing the spec file %s: %v", configPath, err)
	}

	// Check target config
	checkStartWith(got.TargetType, "Kubernetes-", t)
	checkStartWith(got.ProbeCategory, "CloudNative-", t)
	// The target name should be the one from the config file
	check(got.TargetIdentifier, "cluster-foo", t)
	check(got.TargetPassword, "defaultPassword", t)
	check(got.TargetUsername, "defaultUser", t)
}

func TestParseK8sTAPServiceSpecWithOldConfig(t *testing.T) {
	defaultTargetName := "target-foo"
	configPath := "../test/config/turbo-config-old"

	got, err := ParseK8sTAPServiceSpec(configPath, defaultTargetName)

	if err != nil {
		t.Fatalf("Error while parsing the spec file %s: %v", configPath, err)
	}

	// Check target config
	checkStartWith(got.TargetType, "tt1", t)
	checkStartWith(got.ProbeCategory, "pc1", t)
	check(got.TargetIdentifier, "addr1", t)
	// The files of username and password are ignored when parsing the json file
	check(got.TargetPassword, "defaultPassword", t)
	check(got.TargetUsername, "defaultUser", t)
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
		UseVMWare                 bool
		wantStitchingPropertyType stitching.StitchingPropertyType
	}{
		{
			name:                      "test-use-vmware",
			UseVMWare:                 false,
			wantStitchingPropertyType: stitching.IP,
		},
		{
			name:                      "test-not-use-vmware",
			UseVMWare:                 true,
			wantStitchingPropertyType: stitching.UUID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vmtConfig := NewVMTConfig2().UsingVMWare(tt.UseVMWare)
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
