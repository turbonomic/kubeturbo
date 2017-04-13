package metrics

import (
	"fmt"

	"github.com/turbonomic/kubeturbo/pkg/turbostore"
)

type EntityMetricSink struct {
	data *turbostore.Cache
}

func NewEntityMetricSink() *EntityMetricSink {
	return &EntityMetricSink{
		data: turbostore.NewCache(),
	}
}

func (s *EntityMetricSink) AddNewMetricEntry(metric Metric) {
	key := metric.GetUID()
	s.data.Add(key, metric)
}

func (s *EntityMetricSink) UpdateMetricEntry(metric Metric) {
	s.AddNewMetricEntry(metric)
}

func (s *EntityMetricSink) GetMetric(metricUID string) (Metric, error) {
	if m, exist := s.data.Get(metricUID); !exist {
		return nil, fmt.Errorf("Cannot find resource metric for %s", metricUID)
	} else {
		return m.(Metric), nil
	}
}

func (s *EntityMetricSink) MergeSink(anotherSink *EntityMetricSink, filterFunc MetricFilterFunc) {
	for _, key := range s.data.AllKeys() {
		m, exist := s.data.Get(key)
		if !exist {
			continue
		}
		metric, ok := m.(Metric);
		if !ok {
			continue
		}
		if filterFunc != nil && !filterFunc(metric) {
			continue
		}
		s.UpdateMetricEntry(metric)
	}
}
