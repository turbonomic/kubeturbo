package dtofactory

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// Builds GroupDTOs given the map of EntityGroup structures for the kubeturbo probe.
type groupDTOBuilder struct {
	entityGroupMap map[string]*repository.EntityGroup
	targetId       string
}

var (
	ENTITY_TYPE_MAP = map[metrics.DiscoveredEntityType]proto.EntityDTO_EntityType{
		metrics.PodType:       proto.EntityDTO_CONTAINER_POD,
		metrics.ContainerType: proto.EntityDTO_CONTAINER,
	}
)

// New instance of groupDTOBuilder.
// Input parameters are map of discovered EntityGroup instances and
// the target identifier of the kubeturbo probe.
func NewGroupDTOBuilder(entityGroupMap map[string]*repository.EntityGroup,
	targetId string) (*groupDTOBuilder, error) {
	if entityGroupMap == nil || len(entityGroupMap) == 0 {
		return nil, fmt.Errorf("invalid entity group map")
	}
	if targetId == "" {
		return nil, fmt.Errorf("invalid kubeturbo target identifier [%s]", targetId)
	}

	return &groupDTOBuilder{
		entityGroupMap: entityGroupMap,
		targetId:       targetId,
	}, nil
}

// Build groupDTOs
// - for pod and containers that belong to parent group instances for StatefulSets, DaemonSets, ReplicaSets
// - for containers by container name in a pod
// - for pod groups per parent type.
func (builder *groupDTOBuilder) BuildGroupDTOs() []*proto.GroupDTO {
	var result []*proto.GroupDTO

	// Groups per parent instance
	for _, entityGroup := range builder.entityGroupMap {
		if entityGroup.GroupId == "" {
			glog.Errorf("Invalid group id")
			continue
		}

		// This builder expects groups already merged from different tasks. We create only global groups:
		// All pods per parent type.
		// All containers per parent type.
		for etype, members := range entityGroup.Members {
			groupId := fmt.Sprintf("%s/All", entityGroup.GroupId)
			parentGroup := builder.createGroup(entityGroup, etype, groupId, ENTITY_TYPE_MAP[etype], members)
			if parentGroup != nil {
				result = append(result, parentGroup)
			}
		}
	}

	return result
}

// Create a static group for pod or container
func (builder *groupDTOBuilder) createGroup(entityGroup *repository.EntityGroup, entityType metrics.DiscoveredEntityType,
	groupId string, protoType proto.EntityDTO_EntityType, memberList []string) *proto.GroupDTO {

	// group id created using the parent type, name and target identifier
	id := fmt.Sprintf("%s-%s-%s", groupId, builder.targetId, entityType)
	displayName := fmt.Sprintf("%s %ss", groupId, entityType)

	// static group
	groupBuilder := group.StaticGroup(id, group.REGULAR).
		OfType(protoType).
		WithEntities(memberList).
		WithDisplayName(displayName)

	// build group
	groupDTO, err := groupBuilder.Build()
	if err != nil {
		glog.Errorf("Error creating group dto  %s::%s", id, err)
		return nil
	}

	return groupDTO
}
