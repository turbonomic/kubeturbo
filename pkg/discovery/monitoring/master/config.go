package master

import (
	"fmt"

	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"

	restclient "k8s.io/client-go/rest"
)

type ClusterMonitorConfig struct {
	clusterInfoScraper *cluster.ClusterScraper
}

func NewClusterMonitorConfig(kubeConfig *restclient.Config) (*ClusterMonitorConfig, error) {
	clusterInfoScraper, err := cluster.NewClusterInfoScraper(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("Invalid API configuration: %v", err)
	}

	return &ClusterMonitorConfig{
		clusterInfoScraper: clusterInfoScraper,
	}, nil
}

// Implement MonitoringWorkerConfig interface.
func (c ClusterMonitorConfig) GetMonitorType() types.MonitorType {
	return types.StateMonitor
}

// Implement MonitoringWorkerConfig interface.
func (c ClusterMonitorConfig) GetMonitoringSource() types.MonitoringSource {
	return types.ClusterSource
}
