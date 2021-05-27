package compliance

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	api "k8s.io/api/core/v1"
)

// Structure to keep track of the schedulable state of the nodes
type NodeSchedulabilityManager struct {
	cluster *repository.ClusterSummary

	// Map of node name and boolean indicating its schedulable state
	schedulableStateMap map[string]bool

	// List of names of all the unschedulable nodes
	unSchedulableNodes []string
}

// Return a new NodeSchedulabilityManager struct.
// Input parameter ClusterSummary holds the list of nodes in the cluster.
func NewNodeSchedulabilityManager(cluster *repository.ClusterSummary) *NodeSchedulabilityManager {
	manager := &NodeSchedulabilityManager{
		cluster:             cluster,
		schedulableStateMap: make(map[string]bool),
	}

	manager.checkNodes()
	return manager
}

// Helper method to build a map for identifying the UIDs of the pods on the unschedulable nodes
func (manager *NodeSchedulabilityManager) buildNodeNameToPodUIDsMap() map[string][]string {
	nodeNameToPodUIDsMap := make(map[string][]string)

	nodeNameToPodMap := manager.cluster.NodeToRunningPods
	for nodeName, schedulable := range manager.schedulableStateMap {
		// skip over pods on schedulable nodes
		if schedulable {
			continue
		}
		podList := nodeNameToPodMap[nodeName]
		var podUIDList []string
		for _, pod := range podList {
			podUIDList = append(podUIDList, string(pod.UID))
		}
		nodeNameToPodUIDsMap[nodeName] = podUIDList
	}

	return nodeNameToPodUIDsMap
}

// Check the schedulable state of all the nodes in the cluster
func (manager *NodeSchedulabilityManager) checkNodes() {

	manager.unSchedulableNodes = nil

	for _, node := range manager.cluster.Nodes {
		// check for schedulable property
		schedulable := util.NodeIsReady(node) && util.NodeIsSchedulable(node)
		manager.schedulableStateMap[node.Name] = schedulable

		if !schedulable {
			manager.unSchedulableNodes = append(manager.unSchedulableNodes, node.Name)
		}
	}
}

// Return if the given node state is in schedulable state
func (manager *NodeSchedulabilityManager) CheckSchedulable(node *api.Node) bool {
	// Retrieve the schedulable state of the node from the schedulable state map
	// If the map is not computed, then first check all the nodes
	if len(manager.schedulableStateMap) == 0 {
		manager.checkNodes()
	}
	return manager.schedulableStateMap[node.Name]
}

// Return if the given node state is in scchedulable state
func (manager *NodeSchedulabilityManager) CheckSchedulableByName(nodeName string) bool {
	if len(manager.schedulableStateMap) == 0 {
		manager.checkNodes()
	}
	return manager.schedulableStateMap[nodeName]
}
