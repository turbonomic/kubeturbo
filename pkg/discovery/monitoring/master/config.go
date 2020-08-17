package master

import (
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
)

type ClusterMonitorConfig struct {
	clusterInfoScraper *cluster.ClusterScraper
}

func NewClusterMonitorConfig(clusterScraper *cluster.ClusterScraper) *ClusterMonitorConfig {
	return &ClusterMonitorConfig{
		clusterInfoScraper: clusterScraper,
	}
}

// Implement MonitoringWorkerConfig interface.
func (c ClusterMonitorConfig) GetMonitorType() types.MonitorType {
	return types.StateMonitor
}

// Implement MonitoringWorkerConfig interface.
func (c ClusterMonitorConfig) GetMonitoringSource() types.MonitoringSource {
	return types.ClusterSource
}
