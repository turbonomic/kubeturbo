package executor

import (
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
)

type ControllerMergeResizer struct {
	TurboK8sActionExecutor
	kubeletClient              *kubeclient.KubeletClient
	k8sVersion                 string

	spec                       *containerResizeSpec
}


func NewControllerMergeResizer(ae TurboK8sActionExecutor, kubeletClient *kubeclient.KubeletClient) *ControllerMergeResizer {
	return &ControllerMergeResizer{
		TurboK8sActionExecutor: ae,
		kubeletClient:          kubeletClient,
	}
}


// Execute executes the container resize action
// The error info will be shown in UI
func (r *ControllerMergeResizer) Execute(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {

	glog.Infof("Executing merge resize: %++v", input.ActionItem)
	return &TurboActionExecutorOutput{
		Succeeded: true,


	}, nil
}
