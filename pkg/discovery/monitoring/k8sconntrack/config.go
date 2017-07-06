package k8sconntrack

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
)

const (
	defaultK8sConntrackPort int64 = 2222
)

// Config for building a K8sConntrack monitor worker.
type K8sConntrackMonitorConfig struct {
	// the port number of K8sConntrack agent running on each node.
	port int64

	// http or https.
	enableHttps bool
}

func NewK8sConntrackMonitorConfig() *K8sConntrackMonitorConfig {
	return &K8sConntrackMonitorConfig{
		port:        defaultK8sConntrackPort,
		enableHttps: false,
	}
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
