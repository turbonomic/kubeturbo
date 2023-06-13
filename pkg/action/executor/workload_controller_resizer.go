package executor

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	k8sapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/turbonomic/kubeturbo/pkg/action/executor/gitops"
	actionutil "github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	discoveryutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/features"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"github.com/turbonomic/kubeturbo/pkg/util"
)

const openShiftDeployerLabel = "openshift.io/deployer-pod-for.name"

type WorkloadControllerResizer struct {
	TurboK8sActionExecutor
	kubeletClient *kubeclient.KubeletClient
	sccAllowedSet map[string]struct{}
	lockMap       *actionutil.ExpirationMap
}

func NewWorkloadControllerResizer(ae TurboK8sActionExecutor, kubeletClient *kubeclient.KubeletClient,
	sccAllowedSet map[string]struct{}, lockMap *actionutil.ExpirationMap) *WorkloadControllerResizer {
	return &WorkloadControllerResizer{
		TurboK8sActionExecutor: ae,
		kubeletClient:          kubeletClient,
		sccAllowedSet:          sccAllowedSet,
		lockMap:                lockMap,
	}
}

// Execute executes the workload controller resize action
// The error info will be shown in UI
func (r *WorkloadControllerResizer) Execute(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	actionItems := input.ActionItems
	// We need to query atleast 1 pod because it contains the node info, which
	// subsequently is needed to get the cpufrequency.
	// TODO(irfanurrehman): This can be slightly erratic as the value conversions will
	// use the node frequency of the queried pod.
	controllerName, kind, namespace, podSpec, managerApp, replicasNum, isOwnerSet, err := r.getWorkloadControllerDetails(actionItems[0])
	if err != nil {
		glog.Errorf("Failed to get workload controller %s/%s details: %v", namespace, controllerName, err)
		return nil, err
	}

	var resizeSpecs []*containerResizeSpec
	for _, item := range actionItems {
		// We use the container resizer for its already implemented utility functions
		cr := NewContainerResizer(r.TurboK8sActionExecutor, r.kubeletClient, r.sccAllowedSet)
		// build resize specification
		spec, err := cr.buildResizeSpec(item, controllerName, podSpec, getContainerIndex(podSpec, item.GetCurrentSE().GetDisplayName()))
		if err != nil {
			glog.Errorf("Failed to build resize spec for the container %v of the workload controller %v/%v due to: %v",
				item.GetCurrentSE().GetDisplayName(), namespace, controllerName, err)
			return &TurboActionExecutorOutput{}, fmt.Errorf("could not find container %v in parents pod template spec. It is likely an injected sidecar", item.GetCurrentSE().GetDisplayName())

		}

		resizeSpecs = append(resizeSpecs, spec)
	}

	// Temporally increase the NS quota if needed && not Gitops && not orm case
	if utilfeature.DefaultFeatureGate.Enabled(features.AllowIncreaseNsQuota4Resizing) &&
		managerApp == nil && !isOwnerSet {
		quotaAccessor := NewQuotaAccessor(r.clusterScraper.Clientset, namespace)
		// The accessor should get the quotas again within the lock to avoid
		// possible race conditions.
		quotas, errOnQuota := quotaAccessor.Get()
		if errOnQuota != nil {
			return &TurboActionExecutorOutput{}, errOnQuota
		}
		hasQuotas := len(quotas) > 0
		if hasQuotas {
			// If this namespace has quota we force the resize actions to
			// become sequential.
			lockHelper, errOnQuota := lockForQuota(namespace, r.lockMap)
			if errOnQuota != nil {
				return &TurboActionExecutorOutput{}, errOnQuota
			}
			defer lockHelper.ReleaseLock()

			desiredPod := buildDesiredPod4QuotaEvaluation(namespace, resizeSpecs, *podSpec)
			errOnQuota = checkQuotas(quotaAccessor, desiredPod, r.lockMap, replicasNum)
			if errOnQuota != nil {
				return &TurboActionExecutorOutput{}, errOnQuota
			}
			defer quotaAccessor.Revert()
		}

	}

	// execute the Action
	err = resizeWorkloadController(
		r.clusterScraper,
		r.ormClient,
		kind,
		controllerName,
		namespace,
		r.k8sClusterId,
		resizeSpecs,
		managerApp,
		r.gitConfig,
	)
	if err != nil {
		glog.Errorf("Failed to execute resize action on the workload controller %s/%s: %v", namespace, controllerName, err)
		return &TurboActionExecutorOutput{}, err
	}
	glog.V(2).Infof("Successfully execute resize action on the workload controller %s/%s.", namespace, controllerName)

	return &TurboActionExecutorOutput{
		Succeeded: true,
	}, nil
}

func getControllerInfo(targetSE *proto.EntityDTO) (string, string, string, error) {
	if targetSE == nil {
		return "", "", "", fmt.Errorf("workload controller action item does not have a valid target entity")
	}

	workloadCntrldata := targetSE.GetWorkloadControllerData()
	if workloadCntrldata == nil {
		return "", "", "", fmt.Errorf("workload controller action item missing controller data")
	}

	var kind string
	cntrlType := workloadCntrldata.GetControllerType()
	switch cntrlType.(type) {
	case *proto.EntityDTO_WorkloadControllerData_DaemonSetData:
		kind = util.KindDaemonSet
	case *proto.EntityDTO_WorkloadControllerData_DeploymentData:
		kind = util.KindDeployment
	case *proto.EntityDTO_WorkloadControllerData_JobData:
		kind = util.KindJob
	case *proto.EntityDTO_WorkloadControllerData_ReplicaSetData:
		kind = util.KindReplicaSet
	case *proto.EntityDTO_WorkloadControllerData_ReplicationControllerData:
		kind = util.KindReplicationController
	case *proto.EntityDTO_WorkloadControllerData_StatefulSetData:
		kind = util.KindStatefulSet
	case *proto.EntityDTO_WorkloadControllerData_CustomControllerData:
		kind = workloadCntrldata.GetCustomControllerData().GetCustomControllerType()
		if kind != util.KindDeploymentConfig {
			return "", "", "", fmt.Errorf("Unexpected ControllerType: %T in EntityDTO_WorkloadControllerData, custom controller type is: %s", cntrlType, kind)
		}
	default:
		return "", "", "", fmt.Errorf("Unexpected ControllerType: %T in EntityDTO_WorkloadControllerData", cntrlType)
	}

	namespace, err := property.GetWorkloadNamespaceFromProperty(targetSE.GetEntityProperties())
	if err != nil {
		return "", "", "", err
	}

	controllerName := targetSE.GetDisplayName()

	return namespace, controllerName, kind, nil
}

func (r *WorkloadControllerResizer) getWorkloadControllerDetails(actionItem *proto.ActionItemDTO) (string,
	string, string, *k8sapi.PodSpec, *repository.K8sApp, int64, bool, error) {
	targetSE := actionItem.GetTargetSE()
	namespace, controllerName, kind, err := getControllerInfo(targetSE)
	if err != nil {
		return "", "", "", nil, nil, 0, false, err
	}
	podSpec, replicasNum, isOwnerSet, err := r.getWorkloadControllerSpec(kind, namespace, controllerName)
	if err != nil {
		return "", "", "", nil, nil, 0, false, err
	}

	return controllerName, kind, namespace, podSpec, property.GetManagerAppFromProperties(targetSE.GetEntityProperties()), replicasNum, isOwnerSet, nil
}

func (r *WorkloadControllerResizer) getWorkloadControllerSpec(parentKind, namespace, name string) (*k8sapi.PodSpec, int64, bool, error) {
	res, err := GetSupportedResUsingKind(parentKind, namespace, name)
	if err != nil {
		return nil, 0, false, err
	}

	obj, err := r.clusterScraper.DynamicClient.Resource(res).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, 0, false, err
	}

	objKind := obj.GetKind()
	podSpecUnstructured, found, err := unstructured.NestedFieldCopy(obj.Object, "spec", "template", "spec")
	if err != nil || !found {
		return nil, 0, false, fmt.Errorf("error retrieving pod template spec from %s %s/%s: %v", objKind, namespace, name, err)
	}

	podSpec := k8sapi.PodSpec{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(podSpecUnstructured.(map[string]interface{}), &podSpec); err != nil {
		return nil, 0, false, fmt.Errorf("error converting unstructured pod spec to typed pod spec for %s %s/%s: %v", objKind, namespace, name, err)
	}

	replicas := int64(0)
	if parentKind == util.KindDaemonSet { // daemonsets do not have replica field
		replicas, found, err = unstructured.NestedInt64(obj.Object, "status", "desiredNumberScheduled")
	} else {
		replicas, found, err = unstructured.NestedInt64(obj.Object, "spec", "replicas")
	}
	if err != nil || !found {
		return nil, 0, false, fmt.Errorf("error retrieving replicas from %s %s: %v", parentKind, name, err)
	}

	_, isOwnerSet := discoveryutil.GetOwnerInfo(obj.GetOwnerReferences())

	return &podSpec, replicas, isOwnerSet, nil
}

func resizeWorkloadController(clusterScraper *cluster.ClusterScraper, ormClient *resourcemapping.ORMClientManager,
	kind, controllerName, namespace, clusterId string, specs []*containerResizeSpec, managerApp *repository.K8sApp,
	gitConfig gitops.GitConfig) error {
	// prepare controllerUpdater
	controllerUpdater, err := newK8sControllerUpdater(clusterScraper, ormClient, kind, controllerName,
		"", namespace, clusterId, managerApp, gitConfig)
	if err != nil {
		glog.Errorf("Failed to create controllerUpdater: %v", err)
		return err
	}

	glog.V(2).Infof("Begin to resize workload controller %s/%s.", controllerUpdater.namespace, controllerUpdater.name)
	err = controllerUpdater.updateWithRetry(&controllerSpec{0, specs})
	if err != nil {
		glog.Errorf("Failed to resize workload controller %s/%s.", controllerUpdater.namespace, controllerUpdater.name)
		return err
	}
	return nil
}

func getContainerIndex(podSpec *k8sapi.PodSpec, containerName string) int {
	// We assume that the pod spec is valid.
	for i, cont := range podSpec.Containers {
		if cont.Name == containerName {
			return i
		}
	}

	glog.V(4).Infof("Match for container %s not found in pod template spec : %v", containerName, podSpec)
	return -1
}

func buildDesiredPod4QuotaEvaluation(namespace string, resizeSpecs []*containerResizeSpec, podSpec k8sapi.PodSpec) *k8sapi.Pod {
	for _, spec := range resizeSpecs {
		if spec == nil || spec.Index < 0 || spec.Index >= len(podSpec.Containers) {
			glog.V(2).Infof("Skip one resize spec, as it is nil or its index is out of range")
			continue
		}

		if len(spec.NewRequest) > 0 && len(podSpec.Containers[spec.Index].Resources.Requests) == 0 {
			podSpec.Containers[spec.Index].Resources.Requests = make(k8sapi.ResourceList)
		}
		if len(spec.NewCapacity) > 0 && len(podSpec.Containers[spec.Index].Resources.Limits) == 0 {
			podSpec.Containers[spec.Index].Resources.Limits = make(k8sapi.ResourceList)
		}
		if newVal, found := spec.NewRequest[k8sapi.ResourceCPU]; found {
			podSpec.Containers[spec.Index].Resources.Requests[k8sapi.ResourceCPU] = newVal
		}
		if newVal, found := spec.NewRequest[k8sapi.ResourceMemory]; found {
			podSpec.Containers[spec.Index].Resources.Requests[k8sapi.ResourceMemory] = newVal
		}
		if newVal, found := spec.NewCapacity[k8sapi.ResourceCPU]; found {
			podSpec.Containers[spec.Index].Resources.Limits[k8sapi.ResourceCPU] = newVal
		}
		if newVal, found := spec.NewCapacity[k8sapi.ResourceMemory]; found {
			podSpec.Containers[spec.Index].Resources.Limits[k8sapi.ResourceMemory] = newVal
		}
	}
	return &k8sapi.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "pod1",
		},
		Spec: podSpec,
	}
}
