package dtofactory

import (
	"fmt"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type NodePoolsGroupDTOBuilder struct {
	cluster  *repository.ClusterSummary
	targetId string
}

func NewNodePoolsGroupDTOBuilder(cluster *repository.ClusterSummary,
	targetId string) *NodePoolsGroupDTOBuilder {
	return &NodePoolsGroupDTOBuilder{
		cluster:  cluster,
		targetId: targetId,
	}
}

func (builder *NodePoolsGroupDTOBuilder) Build() []*proto.GroupDTO {
	nodePools := util.MapNodePoolToNodes(builder.cluster.Nodes, builder.cluster.MachineSetToNodesMap)
	if len(nodePools) == 0 {
		glog.V(3).Infof("No node pools detected.")
		return nil
	}

	var groupDTOs []*proto.GroupDTO
	for poolName, members := range nodePools {
		groupID := fmt.Sprintf("NodePool::%s [%s]", poolName, builder.targetId)
		displayName := fmt.Sprintf("NodePool-%s-%s", poolName, builder.targetId)
		glog.V(3).Infof("Creating Node Pool group: %s belonging to cluster [%s]",
			displayName, builder.cluster.Name)

		// static group
		dto, err := group.StaticNodePool(groupID).
			OfType(proto.EntityDTO_VIRTUAL_MACHINE).
			WithEntities(util.GetUIDs(members)).
			WithDisplayName(displayName).
			WithOwner(builder.cluster.Name).
			Build()
		if err != nil {
			glog.Errorf("Failed to build Node Pool DTO node group %s: %v", groupID, err)
			continue
		}
		groupDTOs = append(groupDTOs, dto)
	}
	return groupDTOs
}
