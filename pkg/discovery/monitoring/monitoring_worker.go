package monitoring

import (
	"errors"
	"fmt"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/monitoring/kubelet"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/monitoring/master"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/task"
)

type MonitorWorkerConfig interface {
	GetMonitorType() types.MonitorType
	GetMonitoringSource() types.MonitoringSource
}

type MonitoringWorker interface {
	Do() (*metrics.EntityMetricSink, error)
	ReceiveTask(task *task.Task)
	GetMonitoringSource() types.MonitoringSource
}

type ResourceMonitoringWorker interface {
	MonitoringWorker
	RetrieveResourceStat() error
}

type StateMonitoringWorker interface {
	MonitoringWorker
	RetrieveClusterStat() error
}

func BuildMonitorWorker(source types.MonitoringSource, config MonitorWorkerConfig, isFullDiscovery bool, discoveryMetricSink *metrics.EntityMetricSink) (MonitoringWorker, error) {
	// Build monitoring client
	switch source {
	case types.KubeletSource:
		kubeletConfig, ok := config.(*kubelet.KubeletMonitorConfig)
		if !ok {
			return nil, errors.New("failed to build a Kubelet monitoring client as the provided config was not a KubeletMonitorConfig")
		}
		return kubelet.NewKubeletMonitor(kubeletConfig, isFullDiscovery, discoveryMetricSink)
	case types.ClusterSource:
		clusterMonitorConfig, ok := config.(*master.ClusterMonitorConfig)
		if !ok {
			return nil, errors.New("failed to build a cluster monitoring client as the provided config was not a ClusterMonitorConfig")
		}
		return master.NewClusterMonitor(clusterMonitorConfig)
	case types.DummySource:
		dummyMonitorConfig, _ := config.(*DummyMonitorConfig)
		return NewDummyMonitor(dummyMonitorConfig)
	default:
		return nil, fmt.Errorf("unsupported monitoring source %s", source)
	}

}
