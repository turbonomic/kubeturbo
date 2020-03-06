package repository

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
)

type EntityGroup struct {
	Members         map[metrics.DiscoveredEntityType][]string
	ParentKind      string
	ParentName      string
	GroupId         string
	ContainerGroups map[string][]string
}

func NewEntityGroup(kind, name, groupId string) *EntityGroup {
	return &EntityGroup{
		GroupId:         groupId,
		ParentKind:      kind,
		ParentName:      name,
		Members:         make(map[metrics.DiscoveredEntityType][]string),
		ContainerGroups: make(map[string][]string),
	}
}

func (group *EntityGroup) AddMember(entityType metrics.DiscoveredEntityType, member string) {
	members, exists := group.Members[entityType]
	if !exists {
		group.Members[entityType] = []string{}
		members = group.Members[entityType]
	}
	members = append(members, member)
	group.Members[entityType] = members
}
