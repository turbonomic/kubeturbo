package k8sconntrack

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	client "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
)

const (
	defaultK8sConntrackPort int64 = 2222
)

// Config for building a K8sConntrack monitor worker.
type K8sConntrackMonitorConfig struct {
	// Kubernetes client to connect to a Kubernetes cluster.
	kubeClient *client.Clientset

	// the port number of K8sConntrack agent running on each node.
	port int64

	// http or https.
	enableHttps bool
}

func NewK8sConntrackMonitorConfig(kubeConfig *restclient.Config) (*K8sConntrackMonitorConfig, error) {
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubeConfig: %s", err)
	}
	return &K8sConntrackMonitorConfig{
		kubeClient:  kubeClient,
		port:        defaultK8sConntrackPort,
		enableHttps: false,
	}, nil
}

// Assign a different port if it is not default port.
func (kcm *K8sConntrackMonitorConfig) WithPort(port int64) *K8sConntrackMonitorConfig {
	kcm.port = port
	return kcm
}

func (kcm *K8sConntrackMonitorConfig) EnableHttps() *K8sConntrackMonitorConfig {
	kcm.enableHttps = true
	return kcm
}

// Implement MonitoringWorkerConfig interface.
func (kcm *K8sConntrackMonitorConfig) GetMonitorType() types.MonitorType {
	return types.ResourceMonitor
}

// Implement MonitoringWorkerConfig interface.
func (kcm *K8sConntrackMonitorConfig) GetMonitoringSource() types.MonitoringSource {
	return types.K8sConntrackSource
}
