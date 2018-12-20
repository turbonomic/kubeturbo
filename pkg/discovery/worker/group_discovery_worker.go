package worker

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	k8sGroupWorkerID string = "ResourceQuotasDiscoveryWorker"
)

// Converts the cluster group objects to Group DTOs
type k8sPolicyGroupDiscoveryWorker struct {
	id      string
	Cluster *repository.ClusterSummary
}

func Newk8sPolicyGroupDiscoveryWorker(cluster *repository.ClusterSummary) *k8sPolicyGroupDiscoveryWorker {
	return &k8sPolicyGroupDiscoveryWorker{
		Cluster: cluster,
		id:      k8sGroupWorkerID,
	}
}

func (worker *k8sPolicyGroupDiscoveryWorker) Do(policyGroupMap map[string]*repository.PolicyGroup,
) ([]*proto.GroupDTO, error) {

	// Create DTOs for each quota entity
	groupDtoBuilder := dtofactory.NewGroupDTOBuilder(policyGroupMap)
	groupDtos, _ := groupDtoBuilder.BuildGroupDTOs()

	return groupDtos, nil
}
