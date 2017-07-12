package kubelet

import (
	restclient "k8s.io/client-go/rest"

	kubeletclient "k8s.io/kubernetes/pkg/kubelet/client"

	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
)

const (
	APIVersion = "v1"

	DefaultKubeletPort        uint = 10255
	DefaultKubeletHttps            = false
	defaultUseServiceAccount       = false
	defaultServiceAccountFile      = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultInClusterConfig         = true
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
func NewKubeletMonitorConfig(kubeConfig *restclient.Config) *KubeletMonitorConfig {
	kubeletConfig := &kubeletclient.KubeletClientConfig{
		Port:            uint(DefaultKubeletPort),
		EnableHttps:     DefaultKubeletHttps,
		TLSClientConfig: kubeConfig.TLSClientConfig,
		BearerToken:     kubeConfig.BearerToken,
	}

	return &KubeletMonitorConfig{
		KubeletClientConfig: kubeletConfig,
	}
}

func (kc *KubeletMonitorConfig) WithPort(port uint) *KubeletMonitorConfig {
	kc.Port = port
	return kc
}

func (kc *KubeletMonitorConfig) EnableHttps(enable bool) *KubeletMonitorConfig {
	kc.KubeletClientConfig.EnableHttps = enable
	return kc
}
