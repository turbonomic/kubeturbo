package worker

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-crd/api/v1alpha1"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	k8sGroupWorkerID   = "GroupsDiscoveryWorker"
	SLOHorizontalScale = "SLOHorizontalScale"
	Service            = "Service"
)

var (
	actionModes = map[v1alpha1.ActionMode]string{
		v1alpha1.Automatic: "AUTOMATIC",
		v1alpha1.Manual:    "MANUAL",
		v1alpha1.Recommend: "RECOMMEND",
		v1alpha1.Disabled:  "DISABLED",
	}
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
	sidecarContainerSpecs, podsWithVolumes []string) ([]*proto.GroupDTO, error) {
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

func (worker *k8sEntityGroupDiscoveryWorker) BuildTurboPolicyDTOsFromPolicyBindings() []*proto.GroupDTO {
	var groupDTOs []*proto.GroupDTO
	for _, policyBinding := range worker.cluster.TurboPolicyBindings {
		switch policyType := policyBinding.GetPolicyType(); policyType {
		case SLOHorizontalScale:
			groupDTO, err := worker.buildSLOHorizontalScalePolicy(policyBinding)
			if err != nil {
				glog.Errorf("Failed to build %v policy: %v.", policyBinding.GetPolicyType(), err)
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
	// Resolve group members
	resolvedSvcIds, err := worker.resolveSLOPolicyTargets(policyBinding)
	if err != nil {
		return nil, err
	}
	// Create policy settings
	settingPolicy, err := worker.createSLOPolicy(policyBinding)
	if err != nil {
		return nil, err
	}
	// Create a dynamic group and applies settings to the group
	displayName := fmt.Sprintf("%v [%v]", policyBinding, worker.targetId)
	return group.
		StaticRegularGroup(policyBinding.GetUID()).
		WithDisplayName(displayName).
		OfType(proto.EntityDTO_SERVICE).
		WithEntities(resolvedSvcIds).
		WithSettingPolicy(settingPolicy).
		Build()
}

// resolveSLOPolicyTargets resolve services specified in the policy targets
func (worker *k8sEntityGroupDiscoveryWorker) resolveSLOPolicyTargets(
	policyBinding *repository.TurboPolicyBinding) ([]string, error) {
	targets := policyBinding.GetTargets()
	if len(targets) == 0 {
		return nil, fmt.Errorf("policy target is empty")
	}
	var names []string
	for _, target := range targets {
		// All targets must be Service
		if target.Kind != Service {
			return nil, fmt.Errorf("target %v is not a service", target)
		}
		names = append(names, target.Name)
	}
	mergedRegExNames := strings.Join(names, "|")
	namespace := policyBinding.GetNamespace()
	var resolvedSvcIds []string
	for service := range worker.cluster.Services {
		if namespace != service.GetNamespace() {
			continue
		}
		if matched, err := regexp.MatchString(mergedRegExNames, service.GetName()); err == nil && matched {
			resolvedSvcIds = append(resolvedSvcIds, string(service.GetUID()))
		} else if err != nil {
			return nil, err
		}
	}
	if len(resolvedSvcIds) == 0 {
		return nil, fmt.Errorf("failed to resolve service specified the policy targets")
	}
	return resolvedSvcIds, nil
}

// createSLOPolicy converts a TurboPolicy into Turbonomic group and policy settings
// The conversion aborts when there is error validating the policy settings
func (worker *k8sEntityGroupDiscoveryWorker) createSLOPolicy(
	policyBinding *repository.TurboPolicyBinding) (*proto.GroupDTO_SettingPolicy_, error) {
	spec := policyBinding.GetSLOHorizontalScaleSpec()
	if spec == nil {
		return nil, fmt.Errorf("the SLOHorizontalScaleSpec is nil")
	}
	if len(spec.Objectives) == 0 {
		return nil, fmt.Errorf("no objective is set for SLO policy")
	}
	settings := group.NewSettingsBuilder()
	minReplicas, maxReplicas, err := validateReplicas(spec.MinReplicas, spec.MaxReplicas)
	if err != nil {
		return nil, err
	}
	if minReplicas != nil {
		settings.AddSetting(group.NewMinReplicasPolicySetting(float32(*minReplicas)))
	}
	if maxReplicas != nil {
		settings.AddSetting(group.NewMaxReplicasPolicySetting(float32(*maxReplicas)))
	}
	var settingValue float32
	for _, setting := range spec.Objectives {
		if err := json.Unmarshal(setting.Value.Raw, &settingValue); err != nil {
			return nil, err
		}
		if settingValue <= 0 {
			return nil, fmt.Errorf("setting %v has a non-positive value %v",
				setting.Name, settingValue)
		}
		switch setting.Name {
		case v1alpha1.ResponseTime:
			settings.AddSetting(group.NewResponseTimeSLOPolicySetting(settingValue))
		case v1alpha1.Transaction:
			settings.AddSetting(group.NewTransactionSLOPolicySetting(settingValue))
		default:
			return nil, fmt.Errorf("unknown objective name %v", setting.Name)
		}
	}
	behavior := spec.Behavior
	if behavior.HorizontalScaleDown != nil {
		mode, err := validateActionMode(*behavior.HorizontalScaleDown)
		if err != nil {
			return nil, err
		}
		settings.AddSetting(group.NewHorizontalScaleDownAutomationPolicySetting(mode))
	}
	if behavior.HorizontalScaleUp != nil {
		mode, err := validateActionMode(*behavior.HorizontalScaleUp)
		if err != nil {
			return nil, err
		}
		settings.AddSetting(group.NewHorizontalScaleUpAutomationPolicySetting(mode))
	}
	displayName := fmt.Sprintf("TurboPolicy::%v on %v [%v]",
		policyBinding.GetPolicyType(), policyBinding, worker.targetId)
	return group.NewSettingPolicyBuilder().
		WithDisplayName(displayName).
		WithName(policyBinding.GetUID()).
		WithSettings(settings.Build()).
		Build()
}

// validateReplicas validates minReplicas and maxReplicas
func validateReplicas(minReplicas *int32, maxReplicas *int32) (*int32, *int32, error) {
	if minReplicas == nil && maxReplicas == nil {
		return nil, nil, nil
	}
	if minReplicas != nil && !isWithinValidRange(*minReplicas) {
		return nil, nil, fmt.Errorf("invalid minReplicas %v. Must be between 1 and 10000 inclusive", *minReplicas)
	}
	if maxReplicas != nil && !isWithinValidRange(*maxReplicas) {
		return nil, nil, fmt.Errorf("invalid maxReplicas %v. Must be between 1 and 10000 inclusive", *maxReplicas)
	}
	if minReplicas != nil && maxReplicas != nil && *minReplicas > *maxReplicas {
		return nil, nil, fmt.Errorf("minReplicas %v is larger than maxReplicas %v", *minReplicas, *maxReplicas)
	}
	return minReplicas, maxReplicas, nil
}

// isWithinValidRange check if the number of replicas is within the valid range of 1 to 10000
func isWithinValidRange(replicas int32) bool {
	if replicas >= 1 && replicas <= 10000 {
		return true
	}
	return false
}

// validateActionMode validates the action mode
// Valid action mode must exist in actionModes map
func validateActionMode(actionMode v1alpha1.ActionMode) (string, error) {
	if mode, ok := actionModes[actionMode]; ok {
		return mode, nil
	}
	return "", fmt.Errorf("unsupported action mode %v", actionMode)
}
