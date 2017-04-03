package monitoring

import (
	"errors"
	"fmt"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/kubelet"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
)

type MonitorWorkerConfig interface {
	isMonitorWorkerConfig()
}

type MonitoringWorker interface {
	RetrieveResourceStat()
	AssignTask(task *task.WorkerTask)
	InitMetricSink(sink *metrics.MetricSink)
}

func BuildMonitorWorker(source string, config MonitorWorkerConfig) (MonitoringWorker, error) {
	// Build monitoring client
	switch source {
	case "Kubelet":
		kubeletConfig, ok := config.(kubelet.KubeletMonitorConfig)
		if !ok {
			return nil, errors.New("Failed to build a Kubelet monitoring client as the provided config was not a KubeletMonitorConfig")
		}
		return kubelet.NewKubeletMonitor(kubeletConfig)
	//case "Prometheus":
	default:
		return nil, fmt.Errorf("Unsupported monitoring source %s", source)
	}

}
