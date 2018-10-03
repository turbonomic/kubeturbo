package executor

import (
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
	kclient "k8s.io/client-go/kubernetes"
)

type TurboActionExecutorInput struct {
	ActionItem *proto.ActionItemDTO
	Pod        *api.Pod
}

type TurboActionExecutorOutput struct {
	Succeeded bool
	OldPod    *api.Pod
	NewPod    *api.Pod
}

type TurboActionExecutor interface {
	Execute(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error)
}

type TurboK8sActionExecutor struct {
	kubeClient *kclient.Clientset
	//lockMap    *util.ExpirationMap
	podManager util.IPodManager
}

func NewTurboK8sActionExecutor(kubeClient *kclient.Clientset, podManager util.IPodManager) TurboK8sActionExecutor {
	return TurboK8sActionExecutor{
		kubeClient: kubeClient,
		podManager: podManager,
	}
}
