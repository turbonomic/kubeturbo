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

// Build groupDTOs for pod and containers that belong to parent group instances for
// StatefulSets, DaemonSets, ReplicaSets.
// In addition also builds pod groups per parent type.
func (builder *groupDTOBuilder) BuildGroupDTOs() ([]*proto.GroupDTO, error) {
	var result []*proto.GroupDTO

	// Groups per parent instance
	for _, entityGroup := range builder.entityGroupMap {
		// Pod and containers members belonging to the group
		for etype, memberList := range entityGroup.Members {
			if entityGroup.GroupId == "" {
				glog.Errorf("Invalid group id")
				continue
			}
			// group id created using the parent type, name and target identifier
			id := fmt.Sprintf("%s-%s[%s]", entityGroup.GroupId, builder.targetId, etype)

			var protoType proto.EntityDTO_EntityType
			if etype == metrics.PodType {
				protoType = proto.EntityDTO_CONTAINER_POD
			} else if etype == metrics.ContainerType {
				protoType = proto.EntityDTO_CONTAINER
			} else {
				glog.Errorf("Invalid member entity type")
				continue
			}

			// static group
			groupBuilder := group.StaticGroup(id).
				OfType(protoType).
				WithEntities(memberList)

			// group display name
			displayName := fmt.Sprintf("%ss By %s [%s]",
				etype, entityGroup.GroupId, builder.targetId)
			groupBuilder.WithDisplayName(displayName)

			// group resize policy - currently only for stateful sets
			_, exists := CONSISTENT_RESIZE_GROUPS[entityGroup.ParentKind]
			if exists && entityGroup.ParentName != "" {
				glog.V(3).Infof("%s: set group to resize consistently\n", entityGroup.GroupId)
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
