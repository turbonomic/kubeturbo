package metrics

import (
	"fmt"
	"reflect"

	"github.ibm.com/turbonomic/kubeturbo/pkg/turbostore"

	"github.com/golang/glog"
)

type EntityMetricSink struct {
	data                *turbostore.Cache
	maxMetricPointsSize int
}

func NewEntityMetricSink() *EntityMetricSink {
	return &EntityMetricSink{
		data: turbostore.NewCache(),
	}
}

func (s *EntityMetricSink) WithMaxMetricPointsSize(maxMetricPointsSize int) *EntityMetricSink {
	s.maxMetricPointsSize = maxMetricPointsSize
	return s
}

// AddNewMetricEntries adds one or more metric entries to sink.
func (s *EntityMetricSink) AddNewMetricEntries(metrics ...Metric) {
	entries := make(map[string]interface{})
	for _, m := range metrics {
		key := m.GetUID()
		entries[key] = m
	}
	s.data.AddAll(entries)
}

func (s *EntityMetricSink) UpdateMetricEntry(metric Metric) {
	existing, exists := s.data.Get(metric.GetUID())
	if exists {
		metric = metric.UpdateValue(existing, s.maxMetricPointsSize)
	}
	s.AddNewMetricEntries(metric)
}

func (s *EntityMetricSink) GetMetric(metricUID string) (Metric, error) {
	if m, exist := s.data.Get(metricUID); !exist {
		return nil, fmt.Errorf("missing metric %s", metricUID)
	} else {
		return m.(Metric), nil
	}
}

func (s *EntityMetricSink) PrintAllKeys() {
	for _, key := range s.data.AllKeys() {
		glog.V(3).Infof("Current sink has an entry with key: %s", key)
	}
}

func (s *EntityMetricSink) MergeSink(anotherSink *EntityMetricSink, filterFunc MetricFilterFunc) {
	for _, key := range anotherSink.data.AllKeys() {
		m, exist := anotherSink.data.Get(key)
		if !exist {
			glog.Errorf("Key %s doesn't exist in current sink.", key)
			continue
		}
		metric, ok := m.(Metric)
		if !ok {
			glog.Errorf("Incorrect type: %v", reflect.TypeOf(metric))
			continue
		}
		if filterFunc != nil && !filterFunc(metric) {
			glog.Errorf("Failed the filterFunc.")
			continue
		}
		s.UpdateMetricEntry(metric)
	}
}

// ClearCache clears all metrics data
func (s *EntityMetricSink) ClearCache() {
	s.data = turbostore.NewCache()
}
