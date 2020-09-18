package features

import (
	"github.com/golang/glog"

	utilfeature "k8s.io/apiserver/pkg/util/feature"
)

const (
	// Every feature gate should add method here following this template:
	//
	// // owner: @username
	// // alpha:
	// MyFeature featuregate.Feature = "MyFeature"

	// owner: @irfanurrehman
	// alpha:
	//
	// Persistent volumes support.
	PersistentVolumes utilfeature.Feature = "PersistentVolumes"
)

func init() {
	if err := utilfeature.DefaultFeatureGate.Add(DefaultKubeturboFeatureGates); err != nil {
		glog.Fatalf("Unexpected error: %v", err)
	}
}

// DefaultKubeturboFeatureGates consists of all known kubeturbo-specific
// feature keys.  To add a new feature, define a key for it above and
// add it here.
// Ref: https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/
// Note: We use the config to feed the values, not the command line params.
var DefaultKubeturboFeatureGates = map[utilfeature.Feature]utilfeature.FeatureSpec{
	PersistentVolumes: {Default: true, PreRelease: utilfeature.Beta},
}
