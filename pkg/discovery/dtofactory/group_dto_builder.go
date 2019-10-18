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
	CONSISTENT_RESIZE_GROUPS = map[string]bool{
		"Deployment":            true,
		"ReplicaSet":            true,
		"ReplicationController": true,
		"StatefulSet":           true,
	}
)

var (
	ENTITY_RESIZE_FLAG_MAP = map[metrics.DiscoveredEntityType]bool{
		metrics.PodType:       false,
		metrics.ContainerType: true,
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

		// create groups for each configured entity type
		for etype, resizeEntityType := range ENTITY_RESIZE_FLAG_MAP {
			groupDTOs := builder.createGroupsByEntityType(entityGroup, etype, resizeEntityType)

			result = append(result, groupDTOs...)
		}
	}

	return result
}

func (builder *groupDTOBuilder) createGroupsByEntityType(entityGroup *repository.EntityGroup,
	entityType metrics.DiscoveredEntityType, resizeEntityType bool) []*proto.GroupDTO {

	var result []*proto.GroupDTO

	// member type
	var protoType proto.EntityDTO_EntityType
	protoType, foundType := ENTITY_TYPE_MAP[entityType]
	if !foundType {
		glog.Errorf("Invalid member entity type %s", entityType)
		return []*proto.GroupDTO{}
	}

	// member list
	memberList, etypeExists := entityGroup.Members[entityType]

	if !etypeExists {
		return []*proto.GroupDTO{}
	}

	// group id for parent group
	groupId := fmt.Sprintf("%s", entityGroup.GroupId)

	// resize setting for entities based on the parent type of the group and if sub groups will be created.
	// If sub groups are created, the resize setting is applied on the sub-groups
	createSubGroups := hasSubGroups(entityGroup)
	resizeFlag := resizeEntityType && consistentResizeGroupType(entityGroup) && !createSubGroups

	fmt.Printf("%s:%s -> %s\n", groupId, entityType, resizeFlag)
	parentGroup := builder.createGroup(entityGroup, entityType, groupId, protoType, memberList, resizeFlag)
	if parentGroup != nil {
		result = append(result, parentGroup)
	}

	// sub groups are created only for containers
	if entityType == metrics.ContainerType {
		// Only one type of container in the pod, so sub groups are not created.
		// In this case, the parent level container group has the consistent resize flag set to true
		if !createSubGroups {
			return result
		}

		subGroups := builder.createSubGroups(entityGroup, entityType)
		result = append(result, subGroups...)
	}

	return result
}

// Create sub groups for different container entities belonging to a pod.
// These group members have resize consistent flag set to true.
func (builder *groupDTOBuilder) createSubGroups(entityGroup *repository.EntityGroup,
	entityType metrics.DiscoveredEntityType) []*proto.GroupDTO {

	var result []*proto.GroupDTO

	// Only one type of container in the pod, so sub groups are not created.
	// In this case, the parent level container group has the consistent resize flag set to true
	if len(entityGroup.ContainerGroups) <= 1 {
		return []*proto.GroupDTO{}
	}

	// Additional sub groups for the different containers running in a pod
	for containerName, containerList := range entityGroup.ContainerGroups {
		etype := metrics.ContainerType
		protoType := proto.EntityDTO_CONTAINER

		groupId := fmt.Sprintf("%s::%s", entityGroup.GroupId, containerName)

		// resize policy setting based on the parent type of the group
		resizeFlag := consistentResizeGroupType(entityGroup)
		subGroup := builder.createGroup(entityGroup, etype, groupId, protoType, containerList, resizeFlag)

		if subGroup != nil {
			result = append(result, subGroup)
		}
	}

	return result
}

// Create a static group for pod or container
func (builder *groupDTOBuilder) createGroup(entityGroup *repository.EntityGroup, entityType metrics.DiscoveredEntityType,
	groupId string, protoType proto.EntityDTO_EntityType,
	memberList []string, resizeFlag bool) *proto.GroupDTO {

	// group id created using the parent type, name and target identifier
	id := fmt.Sprintf("%s-%s[%s]", groupId, builder.targetId, entityType)
	displayName := fmt.Sprintf("%ss By %s [%s]", entityType, groupId, builder.targetId)

	// static group
	groupBuilder := group.StaticGroup(id).
		OfType(protoType).
		WithEntities(memberList).
		WithDisplayName(displayName)

	// resize flag setting
	if resizeFlag {
		glog.V(4).Infof("%s: set group to resize consistently", displayName)
		groupBuilder.ResizeConsistently()
	}

	// build group
	groupDTO, err := groupBuilder.Build()
	if err != nil {
		glog.Errorf("Error creating group dto  %s::%s", id, err)
		return nil
	}

	return groupDTO
}

func consistentResizeGroupType(entityGroup *repository.EntityGroup) bool {
	_, exists := CONSISTENT_RESIZE_GROUPS[entityGroup.ParentKind]
	if !exists {
		return false
	}

	return true
}

// Determine if sub groups will be created for container entities in a group
func hasSubGroups(entityGroup *repository.EntityGroup) bool {
	// There are multiple containers in the pod, so the parent group is not re-sized
	if len(entityGroup.ContainerGroups) > 1 {
		return true
	}

	return false
}
