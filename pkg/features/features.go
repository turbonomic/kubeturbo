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
	// beta:
	//
	// Gitops application support.
	// This gate will enable discovery of gitops pipeline applications and
	// the action execution based on the same.
	GitopsApps featuregate.Feature = "GitopsApps"

	// HonorAzLabelPvAffinity owner: @kevinwang
	// alpha:
	//
	// Honor the region/zone labels of the node.
	// This gate will enable honorinig the labels topology.kubernetes.io/region and topology.kubernetes.io/zone
	// of the node which the pod is currently running on and also enable honoring the PV affninity on a pod move
	HonorAzLabelPvAffinity featuregate.Feature = "HonorAzLabelPvAffinity"

	// GoMemLimit (MemoryOptimisations) owner: @mengding @irfanurrehman
	// alpha:
	// This flag enables below optimisations
	//
	// Go runtime soft memory limit support
	// This gate enables Go runtime soft memory limit as explained in
	// https://pkg.go.dev/runtime/debug#SetMemoryLimit
	//
	// Pagination support for list API calls to API server querying workload controllers
	// Without this feature gate the whole list is requested in a single list API call.
	GoMemLimit featuregate.Feature = "GoMemLimit"

	// AllowIncreaseNsQuota4Resizing owner: @kevinwang
	// alpha:
	//
	// This gate will enable the temporary namespace quota increase when
	// kubeturbo execute a resize action on a workload controller
	AllowIncreaseNsQuota4Resizing featuregate.Feature = "AllowIncreaseNsQuota4Resizing"

	// IgnoreAffinities owner: @irfanurrehman
	// alpha:
	//
	// This gate will simply ignore processing of affinities in the cluster.
	// This is to try out temporarily disable processing of affinities to be able to
	// try out a POV in a customer environment, which is held up because of inefficiency
	// in out code, where affinity processing alone takes a really long time.
	// https://vmturbo.atlassian.net/browse/OM-93635?focusedCommentId=771727
	IgnoreAffinities featuregate.Feature = "IgnoreAffinities"

	// NewAffinityProcessing owner: @irfanurrehman
	// alpha:
	//
	// This gate will use the optimised affinity processing algorithm which in turn
	// will ensure that the affinity processing can happen within a single discovery cycle.
	NewAffinityProcessing featuregate.Feature = "NewAffinityProcessing"

	// ForceDeploymentConfigRollout owner: @irfanurrehman
	// alpha:
	//
	// This gate is used to force the rollout of deployment configs on resize actions, if
	// its found that the "spec.triggers" is explicitly set to empty ie. "[]".
	// This is an explicit type of setup we have seen in some customer environments where
	// users are expected to manually rollout the deployment config after any change to the
	// same. More details in warning note here ->
	// https://docs.openshift.com/container-platform/3.11/dev_guide/deployments/basic_deployment_operations.html#triggers
	ForceDeploymentConfigRollout featuregate.Feature = "ForceDeploymentConfigRollout"

	// SegmentationBasedTopologySpread owner: @irfan-ur-rehman
	// alpha:
	// This feature gate is used for upgraded anti-affinity rule handling for those pods
	// which signify anti-affinity to self and whr the topology key is not hostname
	SegmentationBasedTopologySpread featuregate.Feature = "SegmentationBasedTopologySpread"

	// PeerToPeerAffinityAntiaffinity owner: @irfan-ur-rehman
	// alpha:
	// This feature gate is used for upgraded anti-affinity rule handling where correct handling
	// of pod to pod affinity and anti affinity is achieved. So far the way we handle pod to pod
	// affinity and antiaffinity was flawed and could result in pod moves that break the affinity rules.
	// This feature corrects it.
	PeerToPeerAffinityAntiaffinity featuregate.Feature = "PeerToPeerAffinityAntiaffinity"

	// UseUsageCoreNanoSeconds owner: @mengding
	// alpha:
	// This gate is used to instruct kubeturbo to compute container and pod CPU usage through the cumulative
	// CPUStats.UsageCoreNanoSeconds metrics, instead of the instant CPUStats.UsageNanoCores metrics.
	// In some container runtime environment, the CPUStats.UsageNanoCores may not be available.
	// For more details, see https://pkg.go.dev/k8s.io/kubelet/pkg/apis/stats/v1alpha1#CPUStats
	UseUsageCoreNanoSeconds featuregate.Feature = "UseUsageCoreNanoSeconds"

	// KwokClusterTest owner: @irfan-ur-rehman
	// alpha:
	// This gate is used to instruct kubeturbo to skip some kubelet and kube proxy while discovering
	// the KWOK cluster. For more details on setting up a kwok cluster, see
	// https://ibm-turbo-wiki.fyre.ibm.com/en/CON/Home/Software-Develo__-Home/Mediation-Homepage/K8s-Simulation
	KwokClusterTest featuregate.Feature = "KwokClusterTest"

	// VirtualMachinePodMove owner: @cheuk.lam
	// alpha:
	// This feature gate is used for the support of moving VirtualMachines in the OCP environment.
	VirtualMachinePodMove featuregate.Feature = "VirtualMachinePodMove"

	// VirtualMachinePodResize
	// alpha:
	// This feature gate is used for the support of resizing VirtualMachines in the OCP environment.
	VirtualMachinePodResize featuregate.Feature = "VirtualMachinePodResize"
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
	PersistentVolumes:               {Default: true, PreRelease: featuregate.Beta},
	ThrottlingMetrics:               {Default: true, PreRelease: featuregate.Beta},
	GitopsApps:                      {Default: true, PreRelease: featuregate.Beta},
	HonorAzLabelPvAffinity:          {Default: true, PreRelease: featuregate.Alpha},
	GoMemLimit:                      {Default: true, PreRelease: featuregate.Alpha},
	AllowIncreaseNsQuota4Resizing:   {Default: true, PreRelease: featuregate.Alpha},
	IgnoreAffinities:                {Default: false, PreRelease: featuregate.Alpha},
	NewAffinityProcessing:           {Default: true, PreRelease: featuregate.Beta},
	ForceDeploymentConfigRollout:    {Default: false, PreRelease: featuregate.Alpha},
	SegmentationBasedTopologySpread: {Default: true, PreRelease: featuregate.Beta},
	UseUsageCoreNanoSeconds:         {Default: false, PreRelease: featuregate.Alpha},
	KwokClusterTest:                 {Default: false, PreRelease: featuregate.Alpha},
	PeerToPeerAffinityAntiaffinity:  {Default: true, PreRelease: featuregate.Beta},
	VirtualMachinePodMove:           {Default: false, PreRelease: featuregate.Alpha},
	VirtualMachinePodResize:         {Default: false, PreRelease: featuregate.Alpha},
}
