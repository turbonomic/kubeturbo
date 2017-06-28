package util

import (
	"fmt"
	"errors"

	api "k8s.io/client-go/pkg/api/v1"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
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
