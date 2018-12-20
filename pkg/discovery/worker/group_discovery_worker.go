package worker

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
)

// Converts the cluster quotaEntity and QuotaMetrics objects to create Quota DTOs
type k8sPolicyGroupDiscoveryWorker struct {
	id         string
	Cluster    *repository.ClusterSummary
}

func Newk8sPolicyGroupDiscoveryWorker(cluster *repository.ClusterSummary) *k8sPolicyGroupDiscoveryWorker {
	return &k8sPolicyGroupDiscoveryWorker{
		Cluster:    cluster,
		id:         k8sQuotasWorkerID,
	}
}

func (worker *k8sPolicyGroupDiscoveryWorker) Do(policyGroupMap map[string]*repository.PolicyGroup,
) ([]*proto.GroupDTO, error) {

	// Create DTOs for each quota entity
	groupDtoBuilder := dtofactory.NewGroupDTOBuilder(policyGroupMap)
	groupDtos, _ := groupDtoBuilder.BuildGroupDTOs()
	return groupDtos, nil

}
