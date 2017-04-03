package metrics

import (
	"cadvisor/Godeps/_workspace/src/github.com/golang/glog"
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"
)

type MetricSink struct {
	data map[task.DiscoveredEntityType]*turbostore.Cache
}

func NewMetricSink() *MetricSink {
	return &MetricSink{
		data: make(map[task.DiscoveredEntityType]*turbostore.Cache),
	}
}

func (s *MetricSink) RegisterSinkForEntityType(eType task.DiscoveredEntityType) {
	if _, ok := s.data[eType]; ok {
		glog.Warningf("%s has already been registered to metric sink", eType)
	}
	s.data[eType] = turbostore.NewCache()
}

func (s *MetricSink) AddNewMetricEntry(eType task.DiscoveredEntityType, key string, metric ResourceMetric) {
	s.data[eType].Add(key, metric)
}

func (s *MetricSink) UpdateMetricEntry(eType task.DiscoveredEntityType, key string, metric ResourceMetric) {
	s.AddNewMetricEntry(eType, key, metric)
}

func (s *MetricSink) GetMetric(eType task.DiscoveredEntityType, key string) (ResourceMetric, error) {
	cache, ok := s.data[eType]
	if !ok {
		return ResourceMetric{}, fmt.Errorf("%s is not a registered entity.", eType)
	}

	if m, exist := cache.Get(key); !exist {
		return ResourceMetric{}, fmt.Errorf("Cannot find resource metric for %s %s", eType, key)
	} else {
		metric := m.(ResourceMetric)
		return metric, nil
	}
}
