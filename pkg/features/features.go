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

	// PersistentVolumes owner: @irfanurrehman
	// beta:
	//
	// Persistent volumes support.
	PersistentVolumes featuregate.Feature = "PersistentVolumes"

	// ThrottlingMetrics owner: @irfanurrehman
	// beta:
	//
	// Throttling Metrics support.
	ThrottlingMetrics featuregate.Feature = "ThrottlingMetrics"

	// GitopsApps owner: @irfanurrehman
	// alpha:
	//
	// Gitops application support.
	// This gate will enable discovery of gitops pipeline applications and
	// the action execution based on the same.
	GitopsApps featuregate.Feature = "GitopsApps"

	// HonorRegionZoneLabels owner: @kevinwang
	// alpha:
	//
	// Honor the region/zone labels of the node.
	// This gate will enable honorinig the labels topology.kubernetes.io/region and topology.kubernetes.io/zone
	// of the node which the pod is currently running on and also enable honoring the PV affninity on a pod move
	HonorAzLabelPvAffinity featuregate.Feature = "HonorAzLabelPvAffinity"

	// GoMemLimit owner: @mengding
	// alpha:
	//
	// Go runtime soft memory limit support
	// This gate enables Go runtime soft memory limit as explained in
	// https://pkg.go.dev/runtime/debug#SetMemoryLimit
	GoMemLimit featuregate.Feature = "GoMemLimit"

	// PaginateAPICalls owner: @irfanurehman
	// alpha:
	//
	// Pagination support for list API calls to API server querying workload controllers
	// Without this feature gate the whole list is requested in a single list API call.
	PaginateAPICalls featuregate.Feature = "PaginateAPICalls"
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
	PersistentVolumes:      {Default: true, PreRelease: featuregate.Beta},
	ThrottlingMetrics:      {Default: true, PreRelease: featuregate.Beta},
	GitopsApps:             {Default: false, PreRelease: featuregate.Alpha},
	HonorAzLabelPvAffinity: {Default: true, PreRelease: featuregate.Alpha},
	GoMemLimit:             {Default: false, PreRelease: featuregate.Alpha},
	PaginateAPICalls:       {Default: false, PreRelease: featuregate.Alpha},
}
