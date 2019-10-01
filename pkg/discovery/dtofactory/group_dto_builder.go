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
	nodeNames      []string
	targetId       string
}

var (
	CONSISTENT_RESIZE_GROUPS = map[string]bool{
		"Deployment":            true,
		"ReplicaSet":            true,
		"ReplicationController": true,
		"StatefulSet":           true,
	}
)

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
		return nil, fmt.Errorf("Invalid entity group map")
	}
	if targetId == "" {
		return nil, fmt.Errorf("Invalid kubeturbo target identifier [%s]", targetId)
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
func (builder *groupDTOBuilder) BuildGroupDTOs() ([]*proto.GroupDTO, error) {
	var result []*proto.GroupDTO

	// Groups per parent instance
	for _, entityGroup := range builder.entityGroupMap {
		if entityGroup.GroupId == "" {
			glog.Errorf("Invalid group id")
			continue
		}

		// Groups for the Pod and containers belonging to the group
		for etype, memberList := range entityGroup.Members {

			// group id created using the parent type, name and target identifier
			id := fmt.Sprintf("%s-%s[%s]", entityGroup.GroupId, builder.targetId, etype)
			displayName := fmt.Sprintf("%ss By %s [%s]", etype, entityGroup.GroupId, builder.targetId)

			var protoType proto.EntityDTO_EntityType
			protoType, foundType := ENTITY_TYPE_MAP[etype]
			if !foundType {
				glog.Errorf("Invalid member entity type %s", etype)
				continue
			}

			// static group
			groupBuilder := group.StaticGroup(id).
				OfType(protoType).
				WithEntities(memberList).
				WithDisplayName(displayName)

			// build group
			groupDTO, err := groupBuilder.Build()
			if err != nil {
				glog.Errorf("Error creating group dto  %s::%s", id, err)
				continue
			}

			result = append(result, groupDTO)

			glog.V(4).Infof("groupDTO  : %++v", groupDTO)
		}

		if len(entityGroup.ContainerGroups) <= 1 {
			continue
		}

		// Additional sub groups for the different containers running in a pod
		for containerName, containerList := range entityGroup.ContainerGroups {
			etype := metrics.ContainerType

			groupId := fmt.Sprintf("%s::%s", entityGroup.GroupId, containerName)

			id := fmt.Sprintf("%s-%s[%s]", groupId, builder.targetId, etype)
			displayName := fmt.Sprintf("%ss By %s [%s]", etype, groupId, builder.targetId)
			protoType := proto.EntityDTO_CONTAINER

			// static group
			groupBuilder := group.StaticGroup(id).
				OfType(protoType).
				WithEntities(containerList).
				WithDisplayName(displayName)

			// if parent group resize policy is consistent resize, set it for the container sub-group
			_, exists := CONSISTENT_RESIZE_GROUPS[entityGroup.ParentKind]
			if exists && entityGroup.ParentName != "" {
				glog.V(4).Infof("%s: set group to resize consistently\n", entityGroup.GroupId)
				groupBuilder.ResizeConsistently()
			}

			// build group
			groupDTO, err := groupBuilder.Build()
			if err != nil {
				glog.Errorf("Error creating group dto  %s::%s", id, err)
				continue
			}

			result = append(result, groupDTO)

			glog.V(4).Infof("groupDTO  : %++v", groupDTO)
		}
	}

	return result, nil
}
