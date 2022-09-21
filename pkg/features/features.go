package features

import (
	"github.com/golang/glog"

	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
)

const (
	// Every feature gate should add method here following this template:
	//
	// // owner: @username
	// // alpha:
	// MyFeature featuregate.Feature = "MyFeature"

	// owner: @irfanurrehman
	// beta:
	//
	// Persistent volumes support.
	PersistentVolumes featuregate.Feature = "PersistentVolumes"

	// owner: @irfanurrehman
	// beta:
	//
	// Throttling Metrics support.
	ThrottlingMetrics featuregate.Feature = "ThrottlingMetrics"

	// owner: @irfanurrehman
	// alpha:
	//
	// Gitops application support.
	// This gate will enable discovery of gitops pipeline applications and
	// the action execution based on the same.
	GitopsApps featuregate.Feature = "GitopsApps"

	// owner: @kevinwang
	// alpha:
	//
	// Honor the region/zone labels of the node.
	// This gate will enable honorinig the labels topology.kubernetes.io/region and "topology.kubernetes.io/zone
	// of the node which the pod is currently running on
	HonorRegionZoneLabels featuregate.Feature = "HonorRegionZoneLabels"
)

func init() {
	if err := utilfeature.DefaultMutableFeatureGate.Add(DefaultKubeturboFeatureGates); err != nil {
		glog.Fatalf("Unexpected error: %v", err)
	}
}

// DefaultKubeturboFeatureGates consists of all known kubeturbo-specific
// feature keys.  To add a new feature, define a key for it above and
// add it here.
// Ref: https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/
// Note: We use the config to feed the values, not the command line params.
var DefaultKubeturboFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	PersistentVolumes:     {Default: true, PreRelease: featuregate.Beta},
	ThrottlingMetrics:     {Default: true, PreRelease: featuregate.Beta},
	GitopsApps:            {Default: false, PreRelease: featuregate.Alpha},
	HonorRegionZoneLabels: {Default: false, PreRelease: featuregate.Alpha},
}
