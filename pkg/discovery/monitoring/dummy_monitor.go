package monitoring

import (
	"sync"
	"time"

	"github.com/golang/glog"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
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
	wg         sync.WaitGroup
}

func NewDummyMonitor(config *DummyMonitorConfig) (*DummyMonitor, error) {
	return &DummyMonitor{
		config:     config,
		metricSink: metrics.NewEntityMetricSink(),
	}, nil
}

func (m *DummyMonitor) reset() {
	m.metricSink = metrics.NewEntityMetricSink()
}

func (m *DummyMonitor) GetMonitoringSource() types.MonitoringSource {
	return types.DummySource
}

func (m *DummyMonitor) ReceiveTask(task *task.Task) {
	m.reset()
}

func (m *DummyMonitor) Do() (*metrics.EntityMetricSink, error) {
	glog.V(4).Infof("%s has started task.", m.GetMonitoringSource())
	err := m.ActualDummyWork()
	if err != nil {
		glog.Errorf("Failed to execute task: %s", err)
		return m.metricSink, err
	}
	glog.V(4).Infof("%s monitor has finished task.", m.GetMonitoringSource())
	return m.metricSink, nil
}

func (m *DummyMonitor) ActualDummyWork() error {
	numWorkers := 4 // some arbitrary number of parallel routines
	for i := 0; i < numWorkers; i++ {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			time.Sleep(time.Second * time.Duration(m.config.TaskRunTime))
		}()
	}
	m.wg.Wait()
	return nil
}
