package kubelet

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
)

type KubeletMonitorConfig struct {
	//a kubeletClient or a kubeletConfig?
	kubeletClient *KubeletClient
}

// Implement MonitoringWorkerConfig interface.
func (c KubeletMonitorConfig) GetMonitorType() types.MonitorType {
	return types.ResourceMonitor
}
func (c KubeletMonitorConfig) GetMonitoringSource() types.MonitoringSource {
	return types.KubeletSource
}

func NewKubeletMonitorConfig(kclient *KubeletClient) *KubeletMonitorConfig {
	return &KubeletMonitorConfig{
		kubeletClient: kclient,
	}
}
