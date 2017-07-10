package kubelet

import (
	restclient "k8s.io/client-go/rest"

	kubeletclient "k8s.io/kubernetes/pkg/kubelet/client"

	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
)

const (
	APIVersion = "v1"

	defaultKubeletPort        = 10255
	disableKubeletHttps       = false
	defaultUseServiceAccount  = false
	defaultServiceAccountFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultInClusterConfig    = true
)

type KubeletMonitorConfig struct {
	*kubeletclient.KubeletClientConfig
}

// Implement MonitoringWorkerConfig interface.
func (c KubeletMonitorConfig) GetMonitorType() types.MonitorType {
	return types.ResourceMonitor
}
func (c KubeletMonitorConfig) GetMonitoringSource() types.MonitoringSource {
	return types.KubeletSource
}

// Create a new Kubelet monitor config using kubeConfig.
// TODO Add port and https later.
func NewKubeletMonitorConfig(kubeConfig *restclient.Config) (*KubeletMonitorConfig, error) {
	kubeletConfig := &kubeletclient.KubeletClientConfig{
		Port:            uint(defaultKubeletPort),
		EnableHttps:     disableKubeletHttps,
		TLSClientConfig: kubeConfig.TLSClientConfig,
		BearerToken:     kubeConfig.BearerToken,
	}

	return &KubeletMonitorConfig{
		KubeletClientConfig: kubeletConfig,
	}, nil
}
