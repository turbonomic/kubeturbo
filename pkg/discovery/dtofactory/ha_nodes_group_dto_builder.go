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

type HANodesGroupDTOBuilder struct {
	cluster  *repository.ClusterSummary
	targetId string
}

func NewHANodesGroupDTOBuilder(cluster *repository.ClusterSummary,
	targetId string) *HANodesGroupDTOBuilder {
	return &HANodesGroupDTOBuilder{
		cluster:  cluster,
		targetId: targetId,
	}
}

func (builder *HANodesGroupDTOBuilder) Build() []*proto.GroupDTO {
	if len(detectors.HANodeRoles) == 0 {
		glog.Infof("HA node groups not specified.")
		return nil
	}
	HANodeGroups := builder.buildHANodeGroups()
	if len(HANodeGroups) == 0 {
		glog.Infof("HA node groups not detected.")
		return nil
	}
	var groupDTOs []*proto.GroupDTO
	for role, members := range HANodeGroups {
		groupID := fmt.Sprintf("HANodes-%s-%s", role, builder.targetId)
		displayName := fmt.Sprintf("HANodes::%s [%s]", role, builder.targetId)
		groupDTO, err := group.DoNotPlaceTogether(groupID).
			WithDisplayName(displayName).
			OnSellerType(proto.EntityDTO_PHYSICAL_MACHINE).
			WithBuyers(group.StaticBuyers(members).OfType(proto.EntityDTO_VIRTUAL_MACHINE).AtMost(1)).
			Build()
		if err != nil {
			glog.Errorf("Failed to build DTO for HA node group %s: %v", groupID, err)
			continue
		}
		glog.V(4).Infof("HA node group: %+v", groupDTO)
		groupDTOs = append(groupDTOs, groupDTO...)
	}
	return groupDTOs
}

// buildHANodeGroups constructs a nodeRole -> nodeList map
// This needs to be done for every discovery as node roles can change
func (builder *HANodesGroupDTOBuilder) buildHANodeGroups() map[string][]string {
	HANodeGroups := map[string][]string{}
	for _, node := range builder.cluster.NodeList {

		// Parse all roles of a node, and add them to a set
		allRoles := util.DetectNodeRoles(node)

		// Get the roles that are defined as HA roles
		HARoles := allRoles.Intersection(detectors.HANodeRoles)
		for _, HARole := range HARoles.List() {
			HANodeGroups[HARole] = append(HANodeGroups[HARole], string(node.UID))
		}
	}
	return HANodeGroups
}
