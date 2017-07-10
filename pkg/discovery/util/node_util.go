package util

import (
	"errors"
	"fmt"

	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/golang/glog"
)

func GetNodeIPForMonitor(node *api.Node, source types.MonitoringSource) (string, error) {
	switch source {
	case types.KubeletSource, types.K8sConntrackSource:
		hostname, ip := node.Name, ""
		for _, addr := range node.Status.Addresses {
			if addr.Type == api.NodeHostName && addr.Address != "" {
				hostname = addr.Address
			}
			if addr.Type == api.NodeInternalIP && addr.Address != "" {
				ip = addr.Address
			}
		}
		if ip != "" {
			return ip, nil
		}
		return "", fmt.Errorf("Node %v has no valid hostname and/or IP address: %v %v", node.Name, hostname, ip)
	default:
		return "", errors.New("Unsupported monitoring source or monitoring source not provided")
	}
}

// Check if a node is in Ready status.
func NodeIsReady(node *api.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == api.NodeReady {
			return condition.Status == api.ConditionTrue
		}
	}
	glog.Errorf("Node %s does not have Ready status.", node.Name)
	return false
}

// Check if a node is schedulable.
func NodeIsSchedulable(node *api.Node) bool {
	return !node.Spec.Unschedulable
}
