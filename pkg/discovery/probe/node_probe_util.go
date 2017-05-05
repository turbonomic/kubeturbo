package probe

import (
	"k8s.io/kubernetes/pkg/api"

	"github.com/golang/glog"
)

// Check if a node is in Ready status.
func nodeIsReady(node *api.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == api.NodeReady {
			return condition.Status == api.ConditionTrue
		}
	}
	glog.Errorf("Node %s does not have Ready status.", node.Name)
	return false
}

// Check if a node is schedulable.
func nodeIsSchedulable(node *api.Node) bool {
	return !node.Spec.Unschedulable
}
