package executor

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	k8sapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"github.com/turbonomic/kubeturbo/pkg/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const openShiftDeployerLabel = "openshift.io/deployer-pod-for.name"

type WorkloadControllerResizer struct {
	TurboK8sActionExecutor
	kubeletClient *kubeclient.KubeletClient
	sccAllowedSet map[string]struct{}
}

func NewWorkloadControllerResizer(ae TurboK8sActionExecutor, kubeletClient *kubeclient.KubeletClient,
	sccAllowedSet map[string]struct{}) *WorkloadControllerResizer {
	return &WorkloadControllerResizer{
		TurboK8sActionExecutor: ae,
		kubeletClient:          kubeletClient,
		sccAllowedSet:          sccAllowedSet,
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
	controllerName, kind, namespace, podSpec, managerApp, err := r.getWorkloadControllerDetails(actionItems[0])
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

	// execute the Action
	err = resizeWorkloadController(
		r.clusterScraper,
		r.ormClient,
		kind,
		controllerName,
		namespace,
		resizeSpecs,
		managerApp,
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

func (r *WorkloadControllerResizer) getWorkloadControllerDetails(actionItem *proto.ActionItemDTO) (string,
	string, string, *k8sapi.PodSpec, *repository.K8sApp, error) {
	targetSE := actionItem.GetTargetSE()
	if targetSE == nil {
		return "", "", "", nil, nil, fmt.Errorf("workload controller action item does not have a valid target entity, %v", actionItem.Uuid)
	}
	workloadCntrldata := targetSE.GetWorkloadControllerData()
	if workloadCntrldata == nil {
		return "", "", "", nil, nil, fmt.Errorf("workload controller action item missing controller data, %v", actionItem.Uuid)
	}

	kind := ""
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
			return "", "", "", nil, nil, fmt.Errorf("Unexpected ControllerType: %T in EntityDTO_WorkloadControllerData, custom controller type is: %s", cntrlType, kind)
		}
	default:
		return "", "", "", nil, nil, fmt.Errorf("Unexpected ControllerType: %T in EntityDTO_WorkloadControllerData", cntrlType)
	}

	namespace, error := property.GetWorkloadNamespaceFromProperty(targetSE.GetEntityProperties())
	if error != nil {
		return "", "", "", nil, nil, error
	}

	controllerName := targetSE.GetDisplayName()
	podSpec, err := r.getWorkloadControllerSpec(kind, namespace, controllerName)
	if err != nil {
		return "", "", "", nil, nil, err
	}

	return controllerName, kind, namespace, podSpec, property.GetManagerAppFromProperties(targetSE.GetEntityProperties()), nil
}

func (r *WorkloadControllerResizer) getWorkloadControllerSpec(parentKind, namespace, name string) (*k8sapi.PodSpec, error) {
	res, err := GetSupportedResUsingKind(parentKind, namespace, name)
	if err != nil {
		return nil, err
	}

	obj, err := r.clusterScraper.DynamicClient.Resource(res).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	objKind := obj.GetKind()
	podSpecUnstructured, found, err := unstructured.NestedFieldCopy(obj.Object, "spec", "template", "spec")
	if err != nil || !found {
		return nil, fmt.Errorf("error retrieving pod template spec from %s %s/%s: %v", objKind, namespace, name, err)
	}

	podSpec := k8sapi.PodSpec{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(podSpecUnstructured.(map[string]interface{}), &podSpec); err != nil {
		return nil, fmt.Errorf("error converting unstructured pod spec to typed pod spec for %s %s/%s: %v", objKind, namespace, name, err)
	}

	return &podSpec, nil
}

func resizeWorkloadController(clusterScraper *cluster.ClusterScraper, ormClient *resourcemapping.ORMClient,
	kind, controllerName, namespace string, specs []*containerResizeSpec, managerApp *repository.K8sApp) error {
	// prepare controllerUpdater
	controllerUpdater, err := newK8sControllerUpdater(clusterScraper,
		ormClient, kind, controllerName, "", namespace, managerApp)
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
