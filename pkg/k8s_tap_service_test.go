package kubeturbo

import (
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
