package repository

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
)

type EntityGroup struct {
	Members    map[metrics.DiscoveredEntityType][]string
	ParentKind string
	GroupId    string
}

func NewEntityGroup(kind, groupId string) *EntityGroup {
	return &EntityGroup{
		GroupId:    groupId,
		ParentKind: kind,
		Members:    make(map[metrics.DiscoveredEntityType][]string),
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
