package compliance

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"k8s.io/apimachinery/pkg/util/sets"
)

// In Turbo, Schedulable nodes are marked with an access commodity 'schedulable'. Pods are required to buy
// this commodity so they can be moved to schedulable nodes
// Unchedulable nodes do not sell this commodity, hence pods will not be moved to these nodes.
// We want to address the issue that pods that are already running on the unschedulable nodes are not moved out
// due to compliance issues. This is done by not requiring the pods already on the unschedulable nodes at the tine
// of discovery to not buy the 'schedulable' commodity so they can continue staying on the current node unless there
// are resource constraints or unless moved by the kubernetes controller.
// In addition, we need pods which do not buy the schedulable commodity to not move to other unschedulable nodes.
// This is achieved by creating an anti-affinity policy for these pods to the other unschedulable nodes other than the
// one it is currently running on.
type UnschedulableNodesAntiAffinityGroupDTOBuilder struct {
	cluster  *repository.ClusterSummary
	targetId string
	// Manager for tracking schedulable nodes
	nodesManager *SchedulableNodeManager
}

func NewUnschedulableNodesAntiAffinityGroupDTOBuilder(cluster *repository.ClusterSummary,
	targetId string, nodesManager *SchedulableNodeManager) *UnschedulableNodesAntiAffinityGroupDTOBuilder {
	return &UnschedulableNodesAntiAffinityGroupDTOBuilder{
		cluster:      cluster,
		targetId:     targetId,
		nodesManager: nodesManager,
	}
}

// Creates DTOs for anti-affinity policy between the pods running on a unschedulable nodes to other unschedulable nodes.
// This will prevent the pods running on a unschedulable nodes to be moved to other other unschedulable nodes.
func (builder *UnschedulableNodesAntiAffinityGroupDTOBuilder) Build() []*proto.GroupDTO {
	var groupDTOs []*proto.GroupDTO

	// Map of unschedulable node to other unschedulable nodes
	unSchedulableNodesGroups := builder.getUnschedulableNodesGroups()

	// Map of unschedulable node name to list of UIDs of pods running on these unschedulable nodes
	nodeToPodsMap := builder.nodesManager.buildNodeToPodMap()

	for unschedulableNodeName, nodeMembers := range unSchedulableNodesGroups {
		// Get the list of pod UIDs of the pods running on the unschedulable node
		podMembers := nodeToPodsMap[unschedulableNodeName]
		groupID := fmt.Sprintf("UnschedulableNodesAntiAffinity-%s-%s", unschedulableNodeName, builder.targetId)
		displayName := fmt.Sprintf("UnschedulableNodesAntiAffinity::%s [%s]", unschedulableNodeName, builder.targetId)

		groupDTO, err := group.DoNotPlace(groupID).
			WithDisplayName(displayName).
			OnSellers(group.StaticSellers(nodeMembers).OfType(proto.EntityDTO_VIRTUAL_MACHINE)).
			WithBuyers(group.StaticBuyers(podMembers).OfType(proto.EntityDTO_CONTAINER_POD)).
			Build()
		if err != nil {
			glog.Errorf("Failed to build DTO for Unschedulable nodes anti affinity group %s: %v", groupID, err)
			continue
		}
		glog.V(3).Infof("Unschedulable nodes anti affinity group: %+v", groupDTO)
		groupDTOs = append(groupDTOs, groupDTO...)
	}

	return groupDTOs
}

// Helper method to map a unschedulable node to the UIDs of other unschedulable nodes
func (builder *UnschedulableNodesAntiAffinityGroupDTOBuilder) getUnschedulableNodesGroups() map[string][]string {

	unSchedulableNodes := builder.nodesManager.unSchedulableNodes

	var unSchedulableNodesByUID []string
	for _, nodeName := range unSchedulableNodes {
		nodeUID := builder.cluster.NodeNameUIDMap[nodeName]
		unSchedulableNodesByUID = append(unSchedulableNodesByUID, nodeUID)
	}
	allNodesSet := sets.NewString(unSchedulableNodesByUID...)

	unschedulableNodesGroups := make(map[string][]string)

	for _, nodeName := range unSchedulableNodes {

		nodeUID := builder.cluster.NodeNameUIDMap[nodeName]
		currNodeSet := sets.NewString(nodeUID)

		// group of the other unschedulable nodes
		otherNodesSet := allNodesSet.Difference(currNodeSet)

		unschedulableNodesGroups[nodeName] = otherNodesSet.List()

		glog.V(4).Infof("%s:%s ---> other unschedulable nodes : [%+v]",
			nodeName, nodeUID, unschedulableNodesGroups[nodeName])

	}

	return unschedulableNodesGroups
}
