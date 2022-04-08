package worker

import (
	"fmt"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	k8sGroupWorkerID string = "GroupsDiscoveryWorker"
)

// Converts the cluster group objects to Group DTOs
type k8sEntityGroupDiscoveryWorker struct {
	id       string
	targetId string
	cluster  *repository.ClusterSummary
}

func Newk8sEntityGroupDiscoveryWorker(cluster *repository.ClusterSummary,
	targetId string) *k8sEntityGroupDiscoveryWorker {
	return &k8sEntityGroupDiscoveryWorker{
		cluster:  cluster,
		id:       k8sGroupWorkerID,
		targetId: targetId,
	}
}

// Group discovery worker collects pod and container groups discovered by different discovery workers.
// It merges the group members belonging to the same group but discovered by different discovery workers.
// Then it creates DTOs for the pod/container groups to be sent to the server.
func (worker *k8sEntityGroupDiscoveryWorker) Do(entityGroupList []*repository.EntityGroup,
	sidecarContainerSpecs []string) ([]*proto.GroupDTO, error) {
	var groupDTOs []*proto.GroupDTO

	// Entity groups per Owner type and instance
	entityGroupMap := make(map[string]*repository.EntityGroup)

	// Combine pod and container members discovered by different
	// discovery workers but belong to the same parent
	for _, entityGroup := range entityGroupList {
		groupId := entityGroup.GroupId
		existingGroup, groupExists := entityGroupMap[groupId]

		if groupExists {
			// groups by entity type
			for etype, newmembers := range entityGroup.Members {
				if _, hasMembers := existingGroup.Members[etype]; hasMembers {
					existingGroup.Members[etype] = append(existingGroup.Members[etype], newmembers...)
				}
			}
		} else {
			entityGroupMap[groupId] = entityGroup
		}
	}

	if glog.V(4) {
		for _, entityGroup := range entityGroupMap {
			glog.Infof("Group --> ::%s::\n", entityGroup.ParentKind)
			for etype, members := range entityGroup.Members {
				glog.Infof("	Members: %s -> %s", etype, members)
			}
		}
	}

	// Create DTOs for each group entity
	groupDtoBuilder, err := dtofactory.NewGroupDTOBuilder(entityGroupMap, worker.targetId)
	if err != nil {
		return nil, err
	}
	groupDTOs = groupDtoBuilder.BuildGroupDTOs()

	// Create DTO for cluster
	clusterGroupDTOs := dtofactory.NewClusterDTOBuilder(worker.cluster, worker.targetId).BuildGroup()
	groupDTOs = append(groupDTOs, clusterGroupDTOs...)

	// Create static groups for HA Nodes
	NodeRolesGroupDTOs := dtofactory.
		NewNodeRolesGroupDTOBuilder(worker.cluster, worker.targetId).
		Build()
	NodePoolsGroupDTOs := dtofactory.
		NewNodePoolsGroupDTOBuilder(worker.cluster, worker.targetId).
		Build()
	groupDTOs = append(groupDTOs, NodeRolesGroupDTOs...)
	groupDTOs = append(groupDTOs, NodePoolsGroupDTOs...)
	groupDTOs = append(groupDTOs, worker.buildSidecarContainerSpecGroup(sidecarContainerSpecs)...)

	return groupDTOs, nil
}

func (worker *k8sEntityGroupDiscoveryWorker) buildSidecarContainerSpecGroup(sidecarContainerSpecs []string) []*proto.GroupDTO {
	var groupsDTOs []*proto.GroupDTO
	if len(sidecarContainerSpecs) <= 0 {
		return groupsDTOs
	}
	id := fmt.Sprintf("Injected Sidecars/All-ContainerSpecs-%s", worker.targetId)
	displayName := "Injected Sidecars/All ContainerSpecs"

	settings := group.NewSettingsBuilder().
		AddSetting(group.NewResizeAutomationPolicySetting("RECOMMEND")).
		Build()

	settingPolicy, err := group.NewSettingPolicyBuilder().
		WithDisplayName(displayName + " Resize Recommend Only " + "[" + worker.targetId + "]").
		WithName(id).
		WithSettings(settings).
		Build()
	if err != nil {
		glog.Errorf("Error creating setting policy dto  %s: %s", id, err)
		return groupsDTOs
	}

	uniqueSpecs := sets.NewString(sidecarContainerSpecs...)
	// static group
	groupBuilder := group.StaticRegularGroup(id).
		OfType(proto.EntityDTO_CONTAINER_SPEC).
		WithEntities(uniqueSpecs.UnsortedList()).
		WithDisplayName(displayName).
		WithSettingPolicy(settingPolicy)

	// build group
	groupDTO, err := groupBuilder.Build()
	if err != nil {
		glog.Errorf("Error creating group dto  %s::%s", id, err)
		return groupsDTOs
	}
	groupsDTOs = append(groupsDTOs, groupDTO)

	return groupsDTOs
}
