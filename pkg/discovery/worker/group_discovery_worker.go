package worker

import (
	"fmt"

	"github.ibm.com/turbonomic/kubeturbo/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	k8sGroupWorkerID = "GroupsDiscoveryWorker"
)

type containerSpecGroupType int

const (
	typeSidecars           containerSpecGroupType = 0
	typeOperatorControlled containerSpecGroupType = 1
	typeSystemNamespaced   containerSpecGroupType = 2
	typeKubeVirt           containerSpecGroupType = 3
	typeUnresizable        containerSpecGroupType = 4
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
	sidecarContainerSpecs, operatorControlledContainerSpecs, systemNameSpaceContainerSpecs,
	podsWithVolumes, notReadyNodes, mirrorPodUids []string, kubeVirtPodUids []string, kubeVirtContainerSpecUids []string,
	topologySpreadConstraintNodesToPods map[string]sets.String, unmovablePods []string, unresizableContainerSpecs []string) ([]*proto.GroupDTO, error) {

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
	groupDTOs = append(groupDTOs, worker.buildContainerSpecGroups(sidecarContainerSpecs, typeSidecars)...)
	// Create static groups for operator controlled containerSpecs
	groupDTOs = append(groupDTOs, worker.buildContainerSpecGroups(operatorControlledContainerSpecs, typeOperatorControlled)...)
	// Create static groups for operator controlled containerSpecs
	groupDTOs = append(groupDTOs, worker.buildContainerSpecGroups(systemNameSpaceContainerSpecs, typeSystemNamespaced)...)

	// Remove entries from the "unresizable" group that are present in other, more meaningful, groups that provide additional
	// context as to why they are in "recommend" mode.
	unresizableSpecs := sets.NewString(unresizableContainerSpecs...)
	unresizableSpecs.Delete(sidecarContainerSpecs...)
	unresizableSpecs.Delete(operatorControlledContainerSpecs...)
	unresizableSpecs.Delete(systemNameSpaceContainerSpecs...)
	unresizableSpecs.Delete(kubeVirtContainerSpecUids...)
	groupDTOs = append(groupDTOs, worker.buildContainerSpecGroups(unresizableSpecs.List(), typeUnresizable)...)

	// Create static groups for all pods that use volumes
	groupDTOs = append(groupDTOs, worker.buildPodsWithVolumesGroup(podsWithVolumes)...)

	groupDTOs = append(groupDTOs, worker.buildNotReadyNodesGroup(notReadyNodes)...)

	groupDTOs = append(groupDTOs, worker.buildMirrorPodGroup(mirrorPodUids)...)

	groupDTOs = append(groupDTOs, worker.buildUnmovablePodsGroup(unmovablePods)...)

	if !utilfeature.DefaultFeatureGate.Enabled(features.VirtualMachinePodMove) {
		// If VM pod move is not enabled, construct a group with RECOMMEND only setting for MOVE
		groupDTOs = append(groupDTOs, worker.buildKubeVirtPodGroup(kubeVirtPodUids)...)
	}
	groupDTOs = append(groupDTOs, worker.buildContainerSpecGroups(kubeVirtContainerSpecUids, typeKubeVirt)...)

	// Create dynamic groups for discovered Turbo policies
	groupDTOs = append(groupDTOs, worker.BuildTurboPolicyDTOsFromPolicyBindings()...)

	// Create groups for topology spread constraints
	groupDTOs = append(groupDTOs, worker.buildTopologySpreadConstraintGroups(topologySpreadConstraintNodesToPods)...)

	return groupDTOs, nil
}

func (worker *k8sEntityGroupDiscoveryWorker) buildContainerSpecGroups(specs []string, groupType containerSpecGroupType) []*proto.GroupDTO {
	var groupsDTOs []*proto.GroupDTO
	if len(specs) <= 0 {
		return groupsDTOs
	}
	id := ""
	displayName := ""
	actionCapability := "RECOMMEND"
	policyMsg := " Resize Recommend Only "
	switch groupType {
	case typeSidecars:
		id = fmt.Sprintf("Injected Sidecars/All-ContainerSpecs-%s", worker.targetId)
		displayName = "Injected Sidecars/All ContainerSpecs"
	case typeOperatorControlled:
		id = fmt.Sprintf("Operator Controlled ContainerSpecs-%s", worker.targetId)
		displayName = "Operator Controlled ContainerSpecs"
	case typeSystemNamespaced:
		id = fmt.Sprintf("System Namespaced ContainerSpecs-%s", worker.targetId)
		displayName = "System Namespaced ContainerSpecs"
		actionCapability = "DISABLED"
		policyMsg = " Resize Disabled "
	case typeKubeVirt:
		id = fmt.Sprintf("KubeVirt ContainerSpecs-%s", worker.targetId)
		displayName = "KubeVirt ContainerSpecs"
	case typeUnresizable:
		id = fmt.Sprintf("Unresizable ContainerSpecs-%s", worker.targetId)
		displayName = "Unresizable ContainerSpecs"
	default:
		return groupsDTOs
	}

	settings := group.NewSettingsBuilder().
		AddSetting(group.NewResizeAutomationPolicySetting(actionCapability)).
		Build()

	settingPolicy, err := group.NewSettingPolicyBuilder().
		WithDisplayName(displayName + policyMsg + "[" + worker.targetId + "]").
		WithName(id).
		WithSettings(settings).
		Build()
	if err != nil {
		glog.Errorf("Error creating setting policy dto  %s: %s", id, err)
		return groupsDTOs
	}

	uniqueSpecs := sets.NewString(specs...)
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

func (worker *k8sEntityGroupDiscoveryWorker) buildKubeVirtPodGroup(kubeVirtPodUids []string) (groupsDTOs []*proto.GroupDTO) {
	if len(kubeVirtPodUids) <= 0 {
		return groupsDTOs
	}

	policyMsg := " Move Recommend Only "
	id := fmt.Sprintf("KubeVirt Pods-%s", worker.targetId)
	displayName := "KubeVirt Pods"
	settings := group.NewSettingsBuilder().
		AddSetting(group.NewMoveAutomationPolicySetting("RECOMMEND")).
		Build()

	settingPolicy, err := group.NewSettingPolicyBuilder().
		WithDisplayName(displayName + policyMsg + "[" + worker.targetId + "]").
		WithName(id).
		WithSettings(settings).
		Build()
	if err != nil {
		glog.Errorf("Error creating setting policy dto  %s: %s", id, err)
		return groupsDTOs
	}

	uniqueSpecs := sets.NewString(kubeVirtPodUids...)
	// static group
	groupBuilder := group.StaticRegularGroup(id).
		OfType(proto.EntityDTO_CONTAINER_POD).
		WithEntities(uniqueSpecs.UnsortedList()).
		WithDisplayName(displayName + " " + worker.targetId).
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

func (worker *k8sEntityGroupDiscoveryWorker) buildUnmovablePodsGroup(unmovablePods []string) []*proto.GroupDTO {
	if len(unmovablePods) == 0 {
		return nil
	}

	displayName := "Unmovable ContainerPods"
	name := fmt.Sprintf("%s [%s]", displayName, worker.targetId)
	policyId := fmt.Sprintf("%s Move Recommend Only [%s]", displayName, worker.targetId)

	settings := group.NewSettingsBuilder().
		AddSetting(group.NewMoveAutomationPolicySetting("RECOMMEND")).
		Build()

	settingsPolicy, err := group.NewSettingPolicyBuilder().
		WithDisplayName(policyId).
		WithName(policyId).
		WithSettings(settings).
		Build()

	if err != nil {
		glog.Errorf("Failed to create settings policy %s: %v", policyId, err)
	}

	groupDTO, err := group.StaticRegularGroup(name).
		OfType(proto.EntityDTO_CONTAINER_POD).
		WithEntities(unmovablePods).
		WithDisplayName(displayName).
		WithSettingPolicy(settingsPolicy).
		Build()

	if err != nil {
		glog.Errorf("Failed to create group %s: %v", name, err)
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
				glog.Warningf("Could not build SLO horizontal scale policy: %v", err)
				continue
			}
			groupDTOs = append(groupDTOs, groupDTO)
		case ContainerVerticalScale:
			groupDTO, err := worker.buildContainerVerticalScalePolicy(policyBinding)
			if err != nil {
				glog.Warningf("Could not build container vertical scale policy: %v", err)
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

func (worker *k8sEntityGroupDiscoveryWorker) buildTopologySpreadConstraintGroups(topologySpreadConstraintNodesToPods map[string]sets.String) []*proto.GroupDTO {
	nodeUids := sets.NewString()
	podUids := sets.NewString()

	for nodeName, nodePodUids := range topologySpreadConstraintNodesToPods {
		if nodeUid, exists := worker.cluster.NodeNameUIDMap[nodeName]; exists {
			nodeUids.Insert(nodeUid)
		}
		podUids.Insert(nodePodUids.UnsortedList()...)
	}

	groupDTOs := []*proto.GroupDTO{}
	groupDTOs = append(groupDTOs, worker.buildTopologySpreadConstraintsNodesGroup(nodeUids)...)
	groupDTOs = append(groupDTOs, worker.buildTopologySpreadConstraintsPodsGroup(podUids)...)
	return groupDTOs
}

func (worker *k8sEntityGroupDiscoveryWorker) buildTopologySpreadConstraintsNodesGroup(nodeUids sets.String) []*proto.GroupDTO {
	if len(nodeUids) == 0 {
		return []*proto.GroupDTO{}
	}

	settings := group.NewSettingsBuilder().
		AddSetting(group.NewVirtualMachineSuspendAutomationPolicySettings("DISABLED")).
		Build()

	settingsPolicy, err := group.NewSettingPolicyBuilder().
		WithDisplayName("TopologySpreadConstraint Nodes Suspend Disabled").
		WithName(fmt.Sprintf("TopologySpreadConstraint Nodes Suspend Disabled [%s]", worker.targetId)).
		WithSettings(settings).
		Build()
	if err != nil {
		glog.Errorf("failed to create settings policy DTO: %v", err)
		return []*proto.GroupDTO{}
	}

	groupDTO, err := group.StaticRegularGroup(fmt.Sprintf("TopologySpreadContraint-Nodes-%s", worker.targetId)).
		WithDisplayName("TopologySpreadConstraint Nodes").
		OfType(proto.EntityDTO_VIRTUAL_MACHINE).
		WithEntities(nodeUids.UnsortedList()).
		WithSettingPolicy(settingsPolicy).
		Build()

	if err != nil {
		glog.Errorf("failed to create group of Nodes with Pods defining TopologySpreadConstraint: %v", err)
		return []*proto.GroupDTO{}
	}
	return []*proto.GroupDTO{groupDTO}
}

func (worker *k8sEntityGroupDiscoveryWorker) buildTopologySpreadConstraintsPodsGroup(podUids sets.String) []*proto.GroupDTO {
	if len(podUids) <= 0 {
		return []*proto.GroupDTO{}
	}
	settings := group.NewSettingsBuilder().
		AddSetting(group.NewMoveAutomationPolicySetting("DISABLED")).
		Build()

	settingsPolicy, err := group.NewSettingPolicyBuilder().
		WithDisplayName(fmt.Sprintf("TopologySpreadConstraint Pods Move Disabled [%s]", worker.targetId)).
		WithName(fmt.Sprintf("TopologySpreadContraint Pods-%s", worker.targetId)).
		WithSettings(settings).
		Build()
	if err != nil {
		glog.Errorf("failed to create settings policy DTO: %v", err)
		return []*proto.GroupDTO{}
	}

	groupDTO, err := group.StaticRegularGroup(fmt.Sprintf("TopologySpreadContraint-Pods-%s", worker.targetId)).
		OfType(proto.EntityDTO_CONTAINER_POD).
		WithEntities(podUids.UnsortedList()).
		WithDisplayName("TopologySpreadConstraint Pods").
		WithSettingPolicy(settingsPolicy).
		Build()

	if err != nil {
		glog.Errorf("failed to create group of Pods with TopologySpreadConstraints: %v", err)
		return []*proto.GroupDTO{}
	}
	return []*proto.GroupDTO{groupDTO}
}
