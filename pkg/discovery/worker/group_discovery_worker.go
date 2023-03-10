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
	k8sGroupWorkerID = "GroupsDiscoveryWorker"
)

// Converts the cluster group objects to Group DTOs
type k8sEntityGroupDiscoveryWorker struct {
	id                   string
	targetId             string
	cluster              *repository.ClusterSummary
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
	sidecarContainerSpecs, podsWithVolumes []string, notReadyNodes []string, mirrorPodUids []string) ([]*proto.GroupDTO, error) {

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

	// Create static groups for sidecar containerSpecs
	groupDTOs = append(groupDTOs, worker.buildSidecarContainerSpecGroup(sidecarContainerSpecs)...)
	// Create static groups for all pods that use volumes
	groupDTOs = append(groupDTOs, worker.buildPodsWithVolumesGroup(podsWithVolumes)...)

	groupDTOs = append(groupDTOs, worker.buildNotReadyNodesGroup(notReadyNodes)...)

	groupDTOs = append(groupDTOs, worker.buildMirrorPodGroup(mirrorPodUids)...)

	// Create dynamic groups for discovered Turbo policies
	groupDTOs = append(groupDTOs, worker.BuildTurboPolicyDTOsFromPolicyBindings()...)

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

func (worker *k8sEntityGroupDiscoveryWorker) buildPodsWithVolumesGroup(podsWithVolumes []string) []*proto.GroupDTO {
	var groupsDTOs []*proto.GroupDTO
	if len(podsWithVolumes) <= 0 {
		return groupsDTOs
	}
	id := fmt.Sprintf("All-Pods-Using-Volumes-%s", worker.targetId)
	displayName := "All Pods Using Volumes"

	uniqueSpecs := sets.NewString(podsWithVolumes...)
	// static group
	groupBuilder := group.StaticRegularGroup(id).
		OfType(proto.EntityDTO_CONTAINER_POD).
		WithEntities(uniqueSpecs.UnsortedList()).
		WithDisplayName(displayName)

	// build group
	groupDTO, err := groupBuilder.Build()
	if err != nil {
		glog.Errorf("Error creating group dto  %s::%s", id, err)
		return groupsDTOs
	}
	groupsDTOs = append(groupsDTOs, groupDTO)

	return groupsDTOs
}

func (worker *k8sEntityGroupDiscoveryWorker) buildMirrorPodGroup(mirrorPodUids []string) []*proto.GroupDTO {
	if len(mirrorPodUids) <= 0 {
		return nil
	}

	groupDTO, err := group.StaticRegularGroup(fmt.Sprintf("Mirror-Pods-%s", worker.targetId)).
		OfType(proto.EntityDTO_CONTAINER_POD).
		WithEntities(mirrorPodUids).
		WithDisplayName("Mirror Pods " + worker.targetId).
		Build()

	if err != nil {
		glog.Errorf("Error creating group of mirror pods for target [%s]. Error: %v", worker.targetId, err)
		return nil
	}

	return []*proto.GroupDTO{groupDTO}
}

func (worker *k8sEntityGroupDiscoveryWorker) buildNotReadyNodesGroup(notReadyNodes []string) []*proto.GroupDTO {
	var groupsDTOs []*proto.GroupDTO
	if len(notReadyNodes) <= 0 {
		return groupsDTOs
	}
	id := fmt.Sprintf("NotReady-Nodes-%s", worker.targetId)
	displayName := fmt.Sprintf("NotReady Nodes [%s]", worker.targetId)
	uniqueSpecs := sets.NewString(notReadyNodes...)
	// static group
	groupBuilder := group.StaticRegularGroup(id).
		OfType(proto.EntityDTO_VIRTUAL_MACHINE).
		WithEntities(uniqueSpecs.UnsortedList()).
		WithDisplayName(displayName)

	// build group
	groupDTO, err := groupBuilder.Build()
	if err != nil {
		glog.Errorf("Error creating group dto  %s::%s", id, err)
		return groupsDTOs
	}
	groupsDTOs = append(groupsDTOs, groupDTO)

	return groupsDTOs
}

func (worker *k8sEntityGroupDiscoveryWorker) BuildTurboPolicyDTOsFromPolicyBindings() []*proto.GroupDTO {
	var groupDTOs []*proto.GroupDTO
	for _, policyBinding := range worker.cluster.TurboPolicyBindings {
		switch policyType := policyBinding.GetPolicyType(); policyType {
		case SLOHorizontalScale:
			groupDTO, err := worker.buildSLOHorizontalScalePolicy(policyBinding)
			if err != nil {
				glog.Warningf("Failed to build SLO horizontal scale policy: %v", err)
				continue
			}
			groupDTOs = append(groupDTOs, groupDTO)
		case ContainerVerticalScale:
			groupDTO, err := worker.buildContainerVerticalScalePolicy(policyBinding)
			if err != nil {
				glog.Warningf("Failed to build container vertical scale policy: %v", err)
				continue
			}
			groupDTOs = append(groupDTOs, groupDTO)
		default:
			glog.Warningf("Policy type %v is not supported.", policyType)
		}
	}
	if len(groupDTOs) > 0 {
		glog.Infof("Successfully created %v policy groups", len(groupDTOs))
	}
	return groupDTOs
}

func (worker *k8sEntityGroupDiscoveryWorker) buildSLOHorizontalScalePolicy(
	policyBinding *repository.TurboPolicyBinding) (*proto.GroupDTO, error) {
	return buildSLOHorizontalScalePolicy(worker, policyBinding)
}

func (worker *k8sEntityGroupDiscoveryWorker) buildContainerVerticalScalePolicy(
	policyBinding *repository.TurboPolicyBinding) (*proto.GroupDTO, error) {
	return buildContainerVerticalScalePolicy(worker, policyBinding)
}
