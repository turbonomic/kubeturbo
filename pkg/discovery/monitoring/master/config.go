package master

import (

	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"k8s.io/kubernetes/pkg/client/restclient"
)

type ClusterMonitorConfig struct {
	monitorType types.MonitorType
	source      types.MonitoringSource

	kubeConfig *restclient.Config
}

func NewClusterMonitorConfig(kubeConfig *restclient.Config) *ClusterMonitorConfig {
	return &ClusterMonitorConfig{
		monitorType: types.StateMonitor,
		source:      types.ClusterSource,

		kubeConfig: kubeConfig,
	}
}

// Implement MonitoringWorkerConfig interface.
func (c ClusterMonitorConfig) GetMonitorType() types.MonitorType {
	return c.monitorType
}
func (c ClusterMonitorConfig) GetMonitoringSource() types.MonitoringSource {
	return c.source
}
