package kubelet

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
)

type KubeletMonitorConfig struct {
	kubeletClient *kubeclient.KubeletClient
}

// Implement MonitoringWorkerConfig interface.
func (c KubeletMonitorConfig) GetMonitorType() types.MonitorType {
	return types.ResourceMonitor
}
func (c KubeletMonitorConfig) GetMonitoringSource() types.MonitoringSource {
	return types.KubeletSource
}

func NewKubeletMonitorConfig(kclient *kubeclient.KubeletClient) *KubeletMonitorConfig {
	return &KubeletMonitorConfig{
		kubeletClient: kclient,
	}
}
