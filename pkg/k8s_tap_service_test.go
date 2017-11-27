package kubeturbo

import (
	"k8s.io/client-go/rest"
	"testing"
)

func TestParseK8sTAPServiceSpec(t *testing.T) {
	targetName := "target-foo"
	configPath := "../test/config/turbo-config"

	kubeConfig := &rest.Config{Host: targetName}

	got, err := ParseK8sTAPServiceSpec(configPath, kubeConfig)

	if err != nil {
		t.Errorf("Error while parsing the spec fiel %s: %v", configPath, err)
	}

	// Check target config
	check(got.TargetType, "Kubernetes", t)
	check(got.ProbeCategory, "CloudNative", t)
	check(got.TargetIdentifier, targetName, t)
	check(got.TargetPassword, "defaultPassword", t)
	check(got.TargetUsername, "defaultUser", t)

	// Check comm config
	check(got.TurboServer, "https://127.1.1.1:9444", t)
	check(got.RestAPIConfig.OpsManagerUsername, "foo", t)
	check(got.RestAPIConfig.OpsManagerPassword, "bar", t)
}

func check(got, want string, t *testing.T) {
	if got != want {
		t.Errorf("got: %v, want: %v", got, want)
	}
}
