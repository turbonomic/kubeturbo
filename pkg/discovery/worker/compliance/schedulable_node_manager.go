package compliance

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	api "k8s.io/api/core/v1"
)

type SchedulableNodeManager struct {
	nodes            map[string]*api.Node
	schedulableNodes map[string]bool
}

func NewSchedulableNodeManager(nodes []*api.Node) *SchedulableNodeManager {
	nodeMap := make(map[string]*api.Node)
	for _, node := range nodes {
		nodeMap[node.Name] = node
	}
	return &SchedulableNodeManager{
		nodes:            nodeMap,
		schedulableNodes: make(map[string]bool),
	}
}

func (manager *SchedulableNodeManager) checkNodes() {
	for _, node := range manager.nodes {
		schedulable := util.NodeIsReady(node) && util.NodeIsSchedulable(node)
		manager.schedulableNodes[node.Name] = schedulable
	}
}

func (manager *SchedulableNodeManager) CheckSchedulable(node *api.Node) bool {
	if len(manager.schedulableNodes) == 0 {
		manager.checkNodes()
	}
	return manager.schedulableNodes[node.Name]
}

func (manager *SchedulableNodeManager) CheckSchedulableByName(nodeName string) bool {
	if len(manager.schedulableNodes) == 0 {
		manager.checkNodes()
	}
	return manager.schedulableNodes[nodeName]
}
