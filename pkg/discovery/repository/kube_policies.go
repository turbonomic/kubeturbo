package repository

import (
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
)

type EntityGroup struct {
	Members         map[metrics.DiscoveredEntityType][]string
	ParentKind      string
	ParentName      string
	GroupId         string
	ContainerGroups map[string][]string
}

func NewEntityGroup(kind, name string) (*EntityGroup, error) {
	if kind == "" {
		return nil, fmt.Errorf("invalid parent kind")
	}
	groupKey := kind
	if name != "" {
		groupKey = fmt.Sprintf("%s::%s", kind, name)
	}
	return &EntityGroup{
		GroupId:         groupKey,
		ParentKind:      kind,
		ParentName:      name,
		Members:         make(map[metrics.DiscoveredEntityType][]string),
		ContainerGroups: make(map[string][]string),
	}, nil
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
