package monitoring

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"time"
)

// This implementation is for Test only
type DummyMonitorConfig struct {
	TaskRunTime int
}

func (config DummyMonitorConfig) GetMonitorType() types.MonitorType {
	return types.DummyMonitor
}

func (config DummyMonitorConfig) GetMonitoringSource() types.MonitoringSource {
	return types.DummySource
}

type DummyMonitor struct {
	config     *DummyMonitorConfig
	metricSink *metrics.EntityMetricSink
}

func NewDummyMonitor(config *DummyMonitorConfig) (*DummyMonitor, error) {
	return &DummyMonitor{
		config:     config,
		metricSink: metrics.NewEntityMetricSink()}, nil
}

func (m *DummyMonitor) GetMonitoringSource() types.MonitoringSource {
	return types.ClusterSource
}

func (m *DummyMonitor) ReceiveTask(task *task.Task) {
}

func (m *DummyMonitor) Stop() {
}

func (m *DummyMonitor) Do() *metrics.EntityMetricSink {
	time.Sleep(time.Second * time.Duration(m.config.TaskRunTime))
	return m.metricSink
}
