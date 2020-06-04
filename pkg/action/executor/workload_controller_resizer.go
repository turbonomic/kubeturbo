package executor

import (
	"fmt"

	"github.com/golang/glog"
	k8sapi "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
	kclient "k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/action/util"
	idutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
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

// Execute executes the container resize action
// The error info will be shown in UI
func (r *WorkloadControllerResizer) Execute(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	actionItems := input.ActionItems
	pod := input.Pod

	// Check if the pod privilege is supported
	// We assume that this will be same for all replicas
	if !util.SupportPrivilegePod(pod, r.sccAllowedSet) {
		err := fmt.Errorf("pod %s/%s has unsupported SCC", pod.Namespace, pod.Name)
		glog.Errorf("Failed to execute resize action: %v", err)
		return &TurboActionExecutorOutput{}, err
	}

	var resizeSpecs []*containerResizeSpec
	for _, item := range actionItems {
		// We use the container resizer for its already implemented utility functions
		cr := NewContainerResizer(r.TurboK8sActionExecutor, r.kubeletClient, r.sccAllowedSet)

		containerId := item.GetCurrentSE().GetId()
		_, containerIndex, err := idutil.ParseContainerId(containerId)
		if err != nil {
			return nil, fmt.Errorf("failed to parse container index to build resizeAction: %v", err)
		}
		// build resize specification
		spec, err := cr.buildResizeSpec(item, pod, containerIndex)
		if err != nil {
			glog.Errorf("Failed to execute resize action: %v", err)
			return &TurboActionExecutorOutput{}, err
		}

		resizeSpecs = append(resizeSpecs, spec)

	}

	// execute the Action
	err := resizeWorkloadController(
		r.kubeClient,
		r.dynamicClient,
		pod,
		resizeSpecs,
	)
	if err != nil {
		return &TurboActionExecutorOutput{}, err
	}

	return &TurboActionExecutorOutput{
		Succeeded: true,
	}, nil
}

func resizeWorkloadController(client *kclient.Clientset, dynClient dynamic.Interface, pod *k8sapi.Pod, specs []*containerResizeSpec) error {
	// prepare controllerUpdater
	controllerUpdater, err := newK8sControllerUpdater(client, dynClient, pod)
	if err != nil {
		glog.Errorf("Failed to create controllerUpdater: %v", err)
		return err
	}
	glog.V(2).Infof("Begin to resize workload controller %s/%s.",
		pod.Namespace, controllerUpdater.controller)
	// execute the action to update resource requirements of the container of interest

	err = controllerUpdater.updateWithRetry(&controllerSpec{0, specs})
	if err != nil {
		glog.V(2).Infof("Failed to resize workload controller %s/%s.",
			pod.Namespace, controllerUpdater.controller)
		return err
	}
	return nil
}
