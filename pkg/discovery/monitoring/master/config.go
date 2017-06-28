package master

import (
	restclient "k8s.io/client-go/rest"

	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
)

type ClusterMonitorConfig struct {
	kubeConfig *restclient.Config
}

func NewClusterMonitorConfig(kubeConfig *restclient.Config) *ClusterMonitorConfig {
	return &ClusterMonitorConfig{
		kubeConfig: kubeConfig,
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
