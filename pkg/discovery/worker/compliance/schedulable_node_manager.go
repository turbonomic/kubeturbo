package compliance

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	api "k8s.io/api/core/v1"
)

// Structure to keep track of the schedulable state of the nodes
type SchedulableNodeManager struct {
	cluster *repository.ClusterSummary

	// Map of node name and boolean indicating its schedulable state
	schedulableStateMap map[string]bool

	// List of unschedulable nodes by node name
	unSchedulableNodes []string
}

// Build a map for identifying the UIDs of the pods on the unschedulable nodes
func (manager *SchedulableNodeManager) buildNodeToPodMap() map[string][]string {
	nodeToPodsMap := make(map[string][]string)

	nodeNameToPodMap := manager.cluster.NodeNameToPodMap
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
		nodeToPodsMap[nodeName] = podUIDList
	}

	return nodeToPodsMap
}

func NewSchedulableNodeManager(cluster *repository.ClusterSummary) *SchedulableNodeManager {
	manager := &SchedulableNodeManager{
		cluster:             cluster,
		schedulableStateMap: make(map[string]bool),
	}

	manager.checkNodes()
	return manager
}

func (manager *SchedulableNodeManager) checkNodes() {
	nodeList := manager.cluster.NodeList
	for _, node := range nodeList {
		// check for schedulable property
		schedulable := util.NodeIsReady(node) && util.NodeIsSchedulable(node)
		manager.schedulableStateMap[node.Name] = schedulable

		if !schedulable {
			manager.unSchedulableNodes = append(manager.unSchedulableNodes, node.Name)
		}
	}
}

func (manager *SchedulableNodeManager) CheckSchedulable(node *api.Node) bool {
	if len(manager.schedulableStateMap) == 0 {
		manager.checkNodes()
	}
	return manager.schedulableStateMap[node.Name]
}

func (manager *SchedulableNodeManager) CheckSchedulableByName(nodeName string) bool {
	if len(manager.schedulableStateMap) == 0 {
		manager.checkNodes()
	}
	return manager.schedulableStateMap[nodeName]
}
