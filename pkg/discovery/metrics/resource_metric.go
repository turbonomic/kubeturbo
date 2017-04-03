package metrics

type ResourceType string

const (
	CPU               ResourceType = "CPU"
	Memory            ResourceType = "Memory"
	CPUProvisioned    ResourceType = "CPUProvisioned"
	MemoryProvisioned ResourceType = "MemoryProvisioned"
	Transaction       ResourceType = "Transaction"
)

// TODO
type MetricType string

const (
	Capacity MetricType = "Capacity"
	Used     MetricType = "Used"
)

type ResourceMetric map[ResourceType]map[MetricType]float64

func NewResourceMetric() ResourceMetric {
	metrics := make(map[ResourceType]map[MetricType]float64)
	return metrics
}

// TODO, do we allow overwrite?
func (m ResourceMetric) SetMetric(rType ResourceType, mType MetricType, value float64) {
	metricsSubMap, exist := m[rType]
	if !exist {
		metricsSubMap = make(map[MetricType]float64)
	}
	metricsSubMap[mType] = value
	m[rType] = metricsSubMap
}