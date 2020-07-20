package executor

import (
	"github.com/openshift/cluster-api/pkg/client/clientset_generated/clientset"
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
	"k8s.io/client-go/dynamic"
	kclient "k8s.io/client-go/kubernetes"
)

type TurboActionExecutorInput struct {
	ActionItems []*proto.ActionItemDTO
	Pod         *api.Pod
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
	kubeClient    *kclient.Clientset
	dynamicClient dynamic.Interface
	cApiClient    *clientset.Clientset
	podManager    util.IPodManager
	ormClient     *resourcemapping.ORMClient
}

func NewTurboK8sActionExecutor(kubeClient *kclient.Clientset, dynamicClient dynamic.Interface, cApiClient *clientset.Clientset,
	podManager util.IPodManager, ormSpec *resourcemapping.ORMClient) TurboK8sActionExecutor {
	return TurboK8sActionExecutor{
		kubeClient:    kubeClient,
		dynamicClient: dynamicClient,
		cApiClient:    cApiClient,
		podManager:    podManager,
		ormClient:     ormSpec,
	}
}
