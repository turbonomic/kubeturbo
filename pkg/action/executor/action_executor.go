package executor

import (
	api "k8s.io/api/core/v1"

	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.ibm.com/turbonomic/kubeturbo/pkg/action/executor/gitops"
	"github.ibm.com/turbonomic/kubeturbo/pkg/action/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	"github.ibm.com/turbonomic/kubeturbo/pkg/resourcemapping"
)

type TurboActionExecutorInput struct {
	ActionItems []*proto.ActionItemDTO
	Pod         *api.Pod
}

type TurboActionExecutorOutput struct {
	Succeeded           bool
	ActionItemsStatuses map[int64]string //  action UUID: success
	OldPod              *api.Pod
	NewPod              *api.Pod
}

type TurboActionExecutor interface {
	Execute(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error)
	ExecuteList(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error)
}

type TurboK8sActionExecutor struct {
	clusterScraper *cluster.ClusterScraper
	podManager     util.IPodManager
	ormClient      *resourcemapping.ORMClientManager
	gitConfig      gitops.GitConfig
	k8sClusterId   string
}

func NewTurboK8sActionExecutor(clusterScraper *cluster.ClusterScraper,
	podManager util.IPodManager, ormClient *resourcemapping.ORMClientManager,
	gitConfig gitops.GitConfig, clusterId string,
) TurboK8sActionExecutor {
	return TurboK8sActionExecutor{
		clusterScraper: clusterScraper,
		podManager:     podManager,
		ormClient:      ormClient,
		gitConfig:      gitConfig,
		k8sClusterId:   clusterId,
	}
}
