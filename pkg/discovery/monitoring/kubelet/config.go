package kubelet

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"k8s.io/client-go/kubernetes"
)

type KubeletMonitorConfig struct {
	kubeletClient *kubeclient.KubeletClient
	// k8s client used to fall back on, in case
	// some kubelet apis are not available.
	kubeClient *kubernetes.Clientset
}

// Implement MonitoringWorkerConfig interface.
func (c KubeletMonitorConfig) GetMonitorType() types.MonitorType {
	return types.ResourceMonitor
}
func (c KubeletMonitorConfig) GetMonitoringSource() types.MonitoringSource {
	return types.KubeletSource
}

func NewKubeletMonitorConfig(kubeletClient *kubeclient.KubeletClient, kubeClient *kubernetes.Clientset) *KubeletMonitorConfig {
	return &KubeletMonitorConfig{
		kubeletClient: kubeletClient,
		kubeClient:    kubeClient,
	}
}
