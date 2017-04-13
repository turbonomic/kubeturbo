package kubelet

import (
	//"net/url"
	//"strconv"
	//
	//"github.com/golang/glog"
	//kube_config "k8s.io/heapster/common/kubernetes"
	//kuberestclient "k8s.io/kubernetes/pkg/client/restclient"
	"k8s.io/kubernetes/pkg/client/restclient"
	kubeletclient "k8s.io/kubernetes/pkg/kubelet/client"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
)

const (
	APIVersion = "v1"

	defaultKubeletPort        = 10255
	defaultKubeletHttps       = false
	defaultUseServiceAccount  = false
	defaultServiceAccountFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	defaultInClusterConfig    = true
)

type KubeletMonitorConfig struct {
	monitorType types.MonitorType
	source      types.MonitoringSource

	*kubeletclient.KubeletClientConfig
}

// Implement MonitoringWorkerConfig interface.
func (c KubeletMonitorConfig) GetMonitorType() types.MonitorType {
	return c.monitorType
}
func (c KubeletMonitorConfig) GetMonitoringSource() types.MonitoringSource {
	return c.source
}

//
// TODO Add port and https later.
func NewKubeletMonitorConfig(kubeConfig *restclient.Config) (*KubeletMonitorConfig, error) {

	kubeletPort := defaultKubeletPort
	kubeletHttps := defaultKubeletHttps
	glog.Infof("Using kubelet port %d", kubeletPort)

	kubeletConfig := &kubeletclient.KubeletClientConfig{
		Port:            uint(kubeletPort),
		EnableHttps:     kubeletHttps,
		TLSClientConfig: kubeConfig.TLSClientConfig,
		BearerToken:     kubeConfig.BearerToken,
	}

	return &KubeletMonitorConfig{
		monitorType: types.ResourceMonitor,
		source:      types.KubeletSource,

		KubeletClientConfig: kubeletConfig,
	}, nil
}
