package compliance

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// In Turbo, Schedulable nodes are marked with an access commodity 'schedulable'. Pods are required to buy
// this commodity so they can be moved to schedulable nodes
// Unchedulable nodes do not sell this commodity, hence pods will not be moved to these nodes.
// We want to address the issue that pods that are already running on the unschedulable nodes are not moved out
// due to compliance issues. This is done by not requiring the pods already on the unschedulable nodes at the time
// of discovery to buy the 'schedulable' commodity. Without the schedulable commodity the pod continues to stay
// on the current node unless there are resource constraints or unless moved by the kubernetes controller.
// In addition, we need pods which do not buy the schedulable commodity to not move to other unschedulable nodes.
// This is achieved by creating an anti-affinity policy for these pods against other unschedulable nodes.
type UnschedulableNodesAntiAffinityGroupDTOBuilder struct {
	cluster  *repository.ClusterSummary
	targetId string
	// Manager for tracking schedulable nodes
	nodesManager *NodeSchedulabilityManager
}

func NewUnschedulableNodesAntiAffinityGroupDTOBuilder(cluster *repository.ClusterSummary,
	targetId string, nodesManager *NodeSchedulabilityManager) *UnschedulableNodesAntiAffinityGroupDTOBuilder {
	return &UnschedulableNodesAntiAffinityGroupDTOBuilder{
		cluster:      cluster,
		targetId:     targetId,
		nodesManager: nodesManager,
	}
}

// Creates DTOs for anti-affinity policy between the pods running on a unschedulable nodes to other unschedulable nodes.
// This will prevent the pods running on a unschedulable nodes to be moved to other other unschedulable nodes.
func (builder *UnschedulableNodesAntiAffinityGroupDTOBuilder) Build() []*proto.GroupDTO {
	var policyDTOs []*proto.GroupDTO

	if len(builder.nodesManager.unSchedulableNodes) <= 1 {
		glog.Info("Zero or one unschedulable node in the cluster, anti-affinity policies will not be created")
		return policyDTOs
	}

	// Map of unschedulable node name to list of UIDs of pods running on these unschedulable nodes
	nodeToPodsMap := builder.nodesManager.buildNodeNameToPodUIDsMap()

	// List of UIDs of all unschedulable nodes
	var nodeUIDs []string // UIDs of all unschedulable nodes
	var nodeUIDtoNameMap = make(map[string]string)
	for _, nodeName := range builder.nodesManager.unSchedulableNodes {
		nodeUID := builder.cluster.NodeNameUIDMap[nodeName]
		nodeUIDs = append(nodeUIDs, nodeUID)
		nodeUIDtoNameMap[nodeUID] = nodeName
	}

	// Iterate over all the unschedulable nodes
	for idx, nodeUID := range nodeUIDs {
		// policy is being created for the pods on this unschedulable node
		nodeName := nodeUIDtoNameMap[nodeUID]

		// pod members on the current unschedulable node for the policy
		podMembers := nodeToPodsMap[nodeName]

		if len(podMembers) == 0 {
			glog.Infof("No pods on unschedulable node %s, skipping anti-affinity policy", nodeName)
			continue
		}

		// node members comprising of other unschedulable nodes for the policy
		var otherNodeUIDs []string
		otherNodeUIDs = append(otherNodeUIDs, nodeUIDs[:idx]...)
		otherNodeUIDs = append(otherNodeUIDs, nodeUIDs[idx+1:]...)

		nodeMembers := otherNodeUIDs

		// policy id and display name
		groupID := fmt.Sprintf("UnschedulableNodesAntiAffinity-%s-%s", nodeName, builder.targetId)
		displayName := fmt.Sprintf("UnschedulableNodesAntiAffinity::%s [%s]", nodeName, builder.targetId)

		// dto for the policy
		groupDTOs, err := group.DoNotPlace(groupID).
			WithDisplayName(displayName).
			OnSellers(group.StaticSellers(nodeMembers).OfType(proto.EntityDTO_VIRTUAL_MACHINE)).
			WithBuyers(group.StaticBuyers(podMembers).OfType(proto.EntityDTO_CONTAINER_POD)).
			Build()
		if err != nil {
			glog.Errorf("Failed to build anti affinity policy DTO for pods on unschedulable node %s: %v", nodeName, err)
			continue
		}
		glog.V(3).Infof("Created anti-affinity policy for pods on the Unschedulable node:%s", nodeName)

		glog.V(4).Infof("Anti-affinity policy for pods on the Unschedulable node: %+v", groupDTOs)

		policyDTOs = append(policyDTOs, groupDTOs...)
	}

	return policyDTOs
}
