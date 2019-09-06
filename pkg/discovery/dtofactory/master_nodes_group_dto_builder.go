package dtofactory

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type masterNodesGroupDTOBuilder struct {
	cluster  *repository.ClusterSummary
	targetId string
}

func NewMasterNodesGroupDTOBuilder(cluster *repository.ClusterSummary,
	targetId string) *masterNodesGroupDTOBuilder {
	return &masterNodesGroupDTOBuilder{
		cluster:  cluster,
		targetId: targetId,
	}
}

func (builder *masterNodesGroupDTOBuilder) Build() *proto.GroupDTO {
	var masterNodes []string

	nodes := builder.cluster.NodeList
	for _, node := range nodes {
		if util.NodeIsMaster(node) {
			nodeID := string(node.UID)
			masterNodes = append(masterNodes, nodeID)
		}
	}

	if len(masterNodes) == 0 {
		glog.Errorf("Master nodes not detected, cannot create group")
		return nil
	}

	// static group
	// group id created using the parent type, name and target identifier
	groupId := "KubeMasterNodes"
	id := fmt.Sprintf("%s-%s", groupId, builder.targetId)

	displayName := fmt.Sprintf("%s[%s]", groupId, builder.targetId)

	groupBuilder := group.StaticGroup(id).
		WithDisplayName(displayName).
		OfType(proto.EntityDTO_VIRTUAL_MACHINE).
		WithEntities(masterNodes)
	groupDTO, err := groupBuilder.Build()
	if err != nil {
		glog.Errorf("Error creating master nodes group dto  %s::%s", id, err)
	}

	glog.V(3).Infof("Master nodes group %++v\n", groupDTO)

	return groupDTO
}
