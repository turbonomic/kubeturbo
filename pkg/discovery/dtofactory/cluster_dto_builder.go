package dtofactory

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type clusterDTOBuilder struct {
	cluster  *repository.ClusterSummary
	targetId string
}

func NewClusterDTOBuilder(cluster *repository.ClusterSummary,
	targetId string) *clusterDTOBuilder {
	return &clusterDTOBuilder{
		cluster:  cluster,
		targetId: targetId,
	}
}

func (builder *clusterDTOBuilder) Build() []*proto.GroupDTO {
	var members []string
	for _, node := range builder.cluster.NodeList {
		members = append(members, string(node.UID))
	}
	var clusterDTOs []*proto.GroupDTO
	clusterDTO, err := group.Cluster(builder.targetId).
		OfType(proto.EntityDTO_VIRTUAL_MACHINE).
		WithEntities(members).
		WithDisplayName(builder.targetId).
		Build()
	if err != nil {
		glog.Errorf("Failed to build cluster DTO %v", err)
	} else {
		clusterDTOs = append(clusterDTOs, clusterDTO)
	}
	// Create a regular group of VM without cluster constraint
	// This group can be removed when the server is upgraded to 7.21.2+
	vmGroupID := fmt.Sprintf("%s-%s", builder.targetId, metrics.ClusterType)
	vmGroupDisplayName := fmt.Sprintf("%s/%s", metrics.ClusterType, builder.targetId)
	vmGroupDTO, err := group.StaticGroup(vmGroupID).
		OfType(proto.EntityDTO_VIRTUAL_MACHINE).
		WithEntities(members).
		WithDisplayName(vmGroupDisplayName).
		Build()
	if err != nil {
		glog.Errorf("Failed to build VM group DTO %v", err)
	} else {
		clusterDTOs = append(clusterDTOs, vmGroupDTO)
	}
	return clusterDTOs
}
