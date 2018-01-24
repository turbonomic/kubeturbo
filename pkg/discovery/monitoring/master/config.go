package master

import (
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"

	"k8s.io/client-go/kubernetes"
)

type ClusterMonitorConfig struct {
	clusterInfoScraper *cluster.ClusterScraper
}

func NewClusterMonitorConfig(kclient *kubernetes.Clientset) *ClusterMonitorConfig {
	k8sClusterScraper := cluster.NewClusterScraper(kclient)
	return &ClusterMonitorConfig{
		clusterInfoScraper: k8sClusterScraper,
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
