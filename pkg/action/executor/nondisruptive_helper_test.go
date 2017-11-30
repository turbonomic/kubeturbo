package executor

import (
	"testing"

	kclient "k8s.io/client-go/kubernetes"
)

func TestNonDisruptiveHelper_isOperationSupported(t *testing.T) {
	tests := []struct {
		name   string
		helper *NonDisruptiveHelper
		want   bool
	}{
		{name: "DP-k8s-1.5.0", helper: createHelper("Deployment", "1.5.0"), want: false},
		{name: "DP-k8s-1.8.0", helper: createHelper("Deployment", "1.8.0"), want: true},
		{name: "RC-k8s-1.5.0", helper: createHelper("ReplicationController", "1.5.0"), want: true},
		{name: "RC-k8s-1.8.0", helper: createHelper("ReplicationController", "1.8.0"), want: true},
		{name: "RS-k8s-1.5.0", helper: createHelper("ReplicaSet", "1.5.0"), want: true},
		{name: "RS-k8s-1.8.0", helper: createHelper("ReplicaSet", "1.8.0"), want: true},
		{name: "UK-k8s-1.5.0", helper: createHelper("foo", "1.5.0"), want: false},
		{name: "UK-k8s-1.8.0", helper: createHelper("foo", "1.8.0"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.helper.isOperationSupported(); got != tt.want {
				t.Errorf("isOperationSupported() = %v, want %v", got, tt.want)
			}
		})
	}
}

func createHelper(contKind, ks8Version string) *NonDisruptiveHelper {
	return NewNonDisruptiveHelper(&kclient.Clientset{}, "ns-1", contKind, "cont", "pod-1", ks8Version)
}
