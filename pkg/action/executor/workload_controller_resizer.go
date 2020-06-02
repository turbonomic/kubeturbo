package executor

import (
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
)

type WorkloadControllerResizer struct {
	TurboK8sActionExecutor
	kubeletClient *kubeclient.KubeletClient
	k8sVersion    string

	spec *containerResizeSpec
}

func NewWorkloadControllerResizer(ae TurboK8sActionExecutor, kubeletClient *kubeclient.KubeletClient) *WorkloadControllerResizer {
	return &WorkloadControllerResizer{
		TurboK8sActionExecutor: ae,
		kubeletClient:          kubeletClient,
	}
}

// Execute executes the container resize action
// The error info will be shown in UI
func (r *WorkloadControllerResizer) Execute(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {

	glog.Infof("Executing merge resize: %++v", input.ActionItem)
	return &TurboActionExecutorOutput{
		Succeeded: true,
	}, nil
}
