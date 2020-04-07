package worker

import (
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	k8sSvcDiscWorkerID string = "ServiceDiscoveryWorker"
)

// Converts the cluster podEntity to create service DTOs
type k8sServiceDiscoveryWorker struct {
	id         string
	Cluster    *repository.ClusterSummary
	stitchType stitching.StitchingPropertyType
}

func Newk8sServiceDiscoveryWorker(cluster *repository.ClusterSummary) *k8sServiceDiscoveryWorker {
	return &k8sServiceDiscoveryWorker{
		Cluster: cluster,
		id:      k8sSvcDiscWorkerID,
	}
}

func (worker *k8sServiceDiscoveryWorker) Do(podEntitiesMap map[string]*repository.KubePod) ([]*proto.EntityDTO, error) {

	svcEntityDTOBuilder := dtofactory.NewServiceEntityDTOBuilder(worker.Cluster.Services, podEntitiesMap)
	svcEntityDTOs, err := svcEntityDTOBuilder.BuildDTOs()
	if err != nil {
		return nil, fmt.Errorf("error while creating service entityDTOs: %v", err)
	}

	return svcEntityDTOs, nil
}
