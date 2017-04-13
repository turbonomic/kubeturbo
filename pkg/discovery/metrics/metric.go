package metrics

import "github.com/turbonomic/kubeturbo/pkg/discovery/task"

type ResourceType string

const (
	CPU               ResourceType = "CPU"
	Memory            ResourceType = "Memory"
	CPUProvisioned    ResourceType = "CPUProvisioned"
	MemoryProvisioned ResourceType = "MemoryProvisioned"
	Transaction       ResourceType = "Transaction"

	Access  ResourceType = "Access"
	Cluster ResourceType = "Cluster"
)

type MetricProp string

const (
	Capacity MetricProp = "Capacity"
	Used     MetricProp = "Used"
)

type Metric interface {
	GetUID() string
	GetValue() interface{}
}

type MetricFilterFunc func(m Metric) bool

type ResourceMetric struct {
	resourceType ResourceType
	metricProp   MetricProp
	value        float64
}

//
func NewResourceMetric(rType ResourceType, mProp MetricProp, v float64) ResourceMetric {
	return ResourceMetric{
		resourceType: rType,
		metricProp:   mProp,
		value:        v,
	}
}

type EntityResourceMetric struct {
	metricUID string

	entityType task.DiscoveredEntityType
	entityID   string

	ResourceMetric
}

func NewEntityResourceMetric(eType task.DiscoveredEntityType, id string, rType ResourceType, mProp MetricProp,
	v float64) EntityResourceMetric {
	return EntityResourceMetric{
		metricUID:      GenerateEntityResourceMetricUID(eType, id, rType, mProp),
		entityType:     eType,
		entityID:       id,
		ResourceMetric: NewResourceMetric(rType, mProp, v),
	}
}

func (m EntityResourceMetric) GetUID() string {
	return m.metricUID
}

func (m EntityResourceMetric) GetValue() interface{} {
	return m.value
}

// Generate the UID for each metric entry based on EntityType, EntityID, ResourceType and MetricType.
func GenerateEntityResourceMetricUID(eType task.DiscoveredEntityType, id string, rType ResourceType, mType MetricProp) string {
	return string(eType) + "-" + id + string(rType) + "-" + string(mType)
}

type EntityStateMetric struct {
	metricUID string

	entityType task.DiscoveredEntityType
	entityID   string

	resourceType ResourceType
	value        interface{}
}

func NewEntityStateMetric(eType task.DiscoveredEntityType, id string, rType ResourceType, v interface{}) EntityStateMetric {
	return EntityStateMetric{
		metricUID:    GenerateEntityStateMetricUID(eType, id, rType),
		entityType:   eType,
		entityID:     id,
		resourceType: rType,
		value:        v,
	}
}

func (m EntityStateMetric) GetUID() string {
	return m.metricUID
}

func (m EntityStateMetric) GetValue() interface{} {
	return m.value
}

// Generate the UID for each metric entry based on EntityType, EntityID, ResourceType and MetricType.
func GenerateEntityStateMetricUID(eType task.DiscoveredEntityType, id string, rType ResourceType) string {
	return string(eType) + "-" + id + string(rType)
}
