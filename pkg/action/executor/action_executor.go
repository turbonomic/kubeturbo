package executor

import (
	"github.com/openshift/machine-api-operator/pkg/generated/clientset/versioned"
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
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
	clusterScraper *cluster.ClusterScraper
	cApiClient     *versioned.Clientset
	podManager     util.IPodManager
	ormClient      *resourcemapping.ORMClient
}

func NewTurboK8sActionExecutor(clusterScraper *cluster.ClusterScraper, cApiClient *versioned.Clientset,
	podManager util.IPodManager, ormSpec *resourcemapping.ORMClient) TurboK8sActionExecutor {
	return TurboK8sActionExecutor{
		clusterScraper: clusterScraper,
		cApiClient:     cApiClient,
		podManager:     podManager,
		ormClient:      ormSpec,
	}
}
