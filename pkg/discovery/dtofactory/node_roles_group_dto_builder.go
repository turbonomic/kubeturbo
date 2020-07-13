package dtofactory

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/detectors"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type NodeRolesGroupDTOBuilder struct {
	cluster  *repository.ClusterSummary
	targetId string
}

func NewNodeRolesGroupDTOBuilder(cluster *repository.ClusterSummary,
	targetId string) *NodeRolesGroupDTOBuilder {
	return &NodeRolesGroupDTOBuilder{
		cluster:  cluster,
		targetId: targetId,
	}
}

func (builder *NodeRolesGroupDTOBuilder) Build() []*proto.GroupDTO {
	nodeGroups := builder.getNodeGroups()
	if len(nodeGroups) == 0 {
		glog.Infof("No node roles detected.")
		return nil
	}

	var groupDTOs []*proto.GroupDTO
	for role, members := range nodeGroups {
		var dtos []*proto.GroupDTO
		var err error
		roleName := role.roleName
		if role.isHA {
			groupID := fmt.Sprintf("NodeRole[HA]-%s-%s", roleName, builder.targetId)
			displayName := fmt.Sprintf("NodeRole[HA]::%s [%s]", roleName, builder.targetId)
			dtos, err = group.DoNotPlaceTogether(groupID).
				WithDisplayName(displayName).
				OnSellerType(proto.EntityDTO_PHYSICAL_MACHINE).
				WithBuyers(group.StaticBuyers(members).OfType(proto.EntityDTO_VIRTUAL_MACHINE).AtMost(1)).
				Build()
			if err != nil {
				glog.Errorf("Failed to build Node Role DTO [HA] node group %s: %v", groupID, err)
				continue
			}
			glog.V(4).Infof("Built Node Role group [HA]: %+v", dtos)
		} else {
			groupID := fmt.Sprintf("NodeRole::%s [%s]", roleName, builder.targetId)
			displayName := fmt.Sprintf("NodeRole-%s-%s", roleName, builder.targetId)
			// static group
			dto, err := group.StaticGroup(groupID).
				OfType(proto.EntityDTO_VIRTUAL_MACHINE).
				WithEntities(members).
				WithDisplayName(displayName).Build()
			if err != nil {
				glog.Errorf("Failed to build Node Role DTO node group %s: %v", groupID, err)
				continue
			}
			dtos = append(dtos, dto)
			glog.V(4).Infof("Built Node Role group: %+v", dtos)
		}
		groupDTOs = append(groupDTOs, dtos...)
	}
	return groupDTOs
}

type nodeRole struct {
	roleName string
	isHA     bool
}

// buildHANodeGroups constructs a nodeRole -> nodeList map
// This needs to be done for every discovery as node roles can change
func (builder *NodeRolesGroupDTOBuilder) getNodeGroups() map[nodeRole][]string {
	nodeGroups := map[nodeRole][]string{}
	for _, node := range builder.cluster.NodeList {
		allRoles := util.DetectNodeRoles(node)
		for _, role := range allRoles.List() {
			nodeRole := nodeRole{
				roleName: role,
				isHA:     detectors.HANodeRoles.Has(role),
			}
			nodeGroups[nodeRole] = append(nodeGroups[nodeRole], string(node.UID))
		}
	}
	return nodeGroups
}
