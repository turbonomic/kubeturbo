package dtofactory

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
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
	nodePools := builder.getNodePools()
	if len(nodePools) == 0 {
		glog.V(3).Infof("No node pools detected.")
		return nil
	}

	var groupDTOs []*proto.GroupDTO
	for poolName, members := range nodePools {
		var dtos []*proto.GroupDTO
		var err error

		groupID := fmt.Sprintf("NodePool::%s [%s]", poolName, builder.targetId)
		displayName := fmt.Sprintf("NodePool-%s-%s", poolName, builder.targetId)
		// static group
		glog.V(3).Infof("Node Pool group: %s belongs to cluster [%s]",
			displayName, builder.cluster.Name)

		dto, err := group.StaticNodePool(groupID).
			OfType(proto.EntityDTO_VIRTUAL_MACHINE).
			WithEntities(members).
			WithDisplayName(displayName).
			WithOwner(builder.cluster.Name).
			Build()
		if err != nil {
			glog.Errorf("Failed to build Node Pool DTO node group %s: %v", groupID, err)
			continue
		}
		dtos = append(dtos, dto)
		groupDTOs = append(groupDTOs, dtos...)

	}
	return groupDTOs
}

func (builder *NodePoolsGroupDTOBuilder) getNodePools() map[string][]string {
	nodePools := make(map[string][]string)
	for _, node := range builder.cluster.Nodes {
		allPools := util.DetectNodePools(node)
		for _, pool := range allPools.List() {
			glog.Infof("%s node in node pool %s", node.Name, pool)
			nodePools[pool] = append(nodePools[pool], string(node.UID))
		}
	}
	return nodePools
}
