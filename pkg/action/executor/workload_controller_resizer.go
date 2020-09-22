package executor

import (
	"fmt"

	"github.com/golang/glog"
	k8sapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"github.com/turbonomic/kubeturbo/pkg/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

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
	controllerName, kind, pod, err := r.getWorkloadControllerPod(actionItems[0])
	if err != nil {
		return nil, err
	}
	input.Pod = pod

	var resizeSpecs []*containerResizeSpec
	for _, item := range actionItems {
		// We use the container resizer for its already implemented utility functions
		cr := NewContainerResizer(r.TurboK8sActionExecutor, r.kubeletClient, r.sccAllowedSet)
		// build resize specification
		spec, err := cr.buildResizeSpec(item, pod, getContainerIndex(pod, item.GetCurrentSE().GetDisplayName()))
		if err != nil {
			glog.Errorf("Failed to execute resize action: %v", err)
			return &TurboActionExecutorOutput{}, err
		}

		resizeSpecs = append(resizeSpecs, spec)
	}

	// execute the Action
	err = resizeWorkloadController(
		r.clusterScraper,
		r.ormClient,
		kind,
		controllerName,
		pod.Name,
		pod.Namespace,
		resizeSpecs,
	)
	if err != nil {
		return &TurboActionExecutorOutput{}, err
	}

	return &TurboActionExecutorOutput{
		Succeeded: true,
	}, nil
}

func (r *WorkloadControllerResizer) getWorkloadControllerPod(actionItem *proto.ActionItemDTO) (string, string, *k8sapi.Pod, error) {
	targetSE := actionItem.GetTargetSE()
	if targetSE == nil {
		return "", "", nil, fmt.Errorf("workload controller action item does not have a valid target entity, %v", actionItem.Uuid)
	}
	workloadCntrldata := targetSE.GetWorkloadControllerData()
	if workloadCntrldata == nil {
		return "", "", nil, fmt.Errorf("workload controller action item missing controller data, %v", actionItem.Uuid)
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
	default:
		return "", "", nil, fmt.Errorf("Unexpected ControllerType: %T in EntityDTO_WorkloadControllerData", cntrlType)
	}

	namespace, error := property.GetWorkloadNamespaceFromProperty(targetSE.GetEntityProperties())
	if error != nil {
		return "", "", nil, error
	}

	parentName := targetSE.GetDisplayName()
	pod, err := r.getChildPod(kind, namespace, parentName)
	if err != nil {
		return "", "", nil, err
	}

	return parentName, kind, pod, nil
}

func (r *WorkloadControllerResizer) getChildPod(parentKind, namespace, name string) (*k8sapi.Pod, error) {
	res, err := GetSupportedResUsingKind(parentKind, namespace, name)
	if err != nil {
		return nil, err
	}

	parent, err := r.clusterScraper.DynamicClient.Resource(res).Namespace(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	parentSelectorUnstructured, found, err := unstructured.NestedFieldCopy(parent.Object, "spec", "selector")
	if err != nil || !found {
		return nil, fmt.Errorf("error retrieving selector from %s %s/%s: %v", parentKind, namespace, name, err)
	}

	parentSelector := metav1.LabelSelector{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(parentSelectorUnstructured.(map[string]interface{}), &parentSelector); err != nil {
		return nil, fmt.Errorf("error converting unstructured selectors to typed selectors for %s %s/%s: %v", parentKind, namespace, name, err)
	}

	// TODO(irfanurrehman): This code does not consider parentSelector.Requirements. Needs revisit.
	// We are in this situation of trying to find a child pod because we need the pods node
	// for cpu frequency conversion.
	// To do this right, consider usage of podlister.
	// https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/client-go/listers/core/v1/pod.go#L30:1
	podsList, err := r.clusterScraper.Clientset.CoreV1().Pods(namespace).List(metav1.ListOptions{LabelSelector: labels.Set(parentSelector.MatchLabels).String()})
	if err != nil {
		return nil, err
	}

	noPodFoundError := fmt.Errorf("could not find any matching pod for %s %s/%s", parentKind, namespace, name)
	if podsList == nil {
		return nil, noPodFoundError
	}
	if len(podsList.Items) < 1 {
		return nil, noPodFoundError
	}

	for _, pod := range podsList.Items {
		if pod.Spec.NodeName != "" {
			// Return the first matching pod with a valid nodeName
			return &pod, err
		}
	}

	return nil, noPodFoundError

}

func resizeWorkloadController(clusterScraper *cluster.ClusterScraper, ormClient *resourcemapping.ORMClient,
	kind, controllerName, podName, namespace string, specs []*containerResizeSpec) error {
	// prepare controllerUpdater
	controllerUpdater, err := newK8sControllerUpdater(clusterScraper, ormClient, kind, controllerName, podName, namespace)
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

func getContainerIndex(pod *k8sapi.Pod, containerName string) int {
	// We assume that the pod spec is valid.
	for i, cont := range pod.Spec.Containers {
		if cont.Name == containerName {
			return i
		}
	}

	return -1
}
