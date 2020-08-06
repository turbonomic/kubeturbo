package metrics

import (
	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
)

type DiscoveredEntityType string

type ResourceType string

const (
	ClusterType     DiscoveredEntityType = "VMCluster"
	NamespaceType   DiscoveredEntityType = "Namespace"
	NodeType        DiscoveredEntityType = "Node"
	ControllerType  DiscoveredEntityType = "Controller"
	PodType         DiscoveredEntityType = "Pod"
	ContainerType   DiscoveredEntityType = "Container"
	ApplicationType DiscoveredEntityType = "Application"
	ServiceType     DiscoveredEntityType = "Service"
)

const (
	CPU                ResourceType = "CPU"
	Memory             ResourceType = "Memory"
	CPURequest         ResourceType = "CPURequest"
	MemoryRequest      ResourceType = "MemoryRequest"
	CPULimitQuota      ResourceType = "CPULimitQuota"
	MemoryLimitQuota   ResourceType = "MemoryLimitQuota"
	CPURequestQuota    ResourceType = "CPURequestQuota"
	MemoryRequestQuota ResourceType = "MemoryRequestQuota"
	CPUProvisioned     ResourceType = "CPUProvisioned"
	MemoryProvisioned  ResourceType = "MemoryProvisioned"
	Transaction        ResourceType = "Transaction"
	NumPods            ResourceType = "NumPods"
	VStorage           ResourceType = "VStorage"

	Access       ResourceType = "Access"
	Cluster      ResourceType = "Cluster"
	CpuFrequency ResourceType = "CpuFrequency"
	Owner        ResourceType = "Owner"
	OwnerType    ResourceType = "OwnerType"
	OwnerUID     ResourceType = "OwnerUID"
)

var (
	// Mapping of Kubernetes API Server resource names to the compute resource types
	KubeComputeResourceTypes = map[v1.ResourceName][]ResourceType{
		v1.ResourceCPU:    {CPU, CPURequest},
		v1.ResourceMemory: {Memory, MemoryRequest},
	}

	// Mapping of Kubernetes API Server resource names to the allocation resource types
	KubeQuotaResourceTypes = map[v1.ResourceName]ResourceType{
		v1.ResourceLimitsCPU:      CPULimitQuota,
		v1.ResourceLimitsMemory:   MemoryLimitQuota,
		v1.ResourceRequestsCPU:    CPURequestQuota,
		v1.ResourceRequestsMemory: MemoryRequestQuota,
	}

	// Mapping of quota to compute resources
	QuotaToComputeMap = map[ResourceType]ResourceType{
		CPULimitQuota:      CPU,
		MemoryLimitQuota:   Memory,
		CPURequestQuota:    CPURequest,
		MemoryRequestQuota: MemoryRequest,
	}

	// Set of resource limit quota types
	QuotaLimitTypes = map[ResourceType]struct{}{
		CPULimitQuota:    {},
		MemoryLimitQuota: {},
	}

	// Set of resource request quota types
	QuotaRequestTypes = map[ResourceType]struct{}{
		CPURequestQuota:    {},
		MemoryRequestQuota: {},
	}

	// List of Compute resources
	ComputeResources []ResourceType

	// List of Quota resources
	QuotaResources []ResourceType

	// List of cpu related metrics
	CPUResources map[ResourceType]bool

	// Mapping of compute to quota resources
	ComputeToQuotaMap map[ResourceType]ResourceType
)

func init() {
	var cpuResources []ResourceType
	// Compute ComputeResources from the KubeComputeResourceTypes map
	for name, resourceList := range KubeComputeResourceTypes {
		ComputeResources = append(ComputeResources, resourceList...)
		if name == v1.ResourceCPU {
			cpuResources = append(cpuResources, resourceList...)
		}
	}
	// Compute QuotaResources from the KubeQuotaResourceTypes map
	for name, resource := range KubeQuotaResourceTypes {
		QuotaResources = append(QuotaResources, resource)
		if name == v1.ResourceLimitsCPU || name == v1.ResourceRequestsCPU {
			cpuResources = append(cpuResources, resource)
		}
	}
	// Compute CPUResources
	CPUResources = make(map[ResourceType]bool)
	for _, resource := range cpuResources {
		CPUResources[resource] = true
	}
	// Compute ComputeToQuotaMap as the inverse of QuotaToComputeMap
	ComputeToQuotaMap = make(map[ResourceType]ResourceType)
	for k, v := range QuotaToComputeMap {
		ComputeToQuotaMap[v] = k
	}
}

// Returns true if the given resource type argument belongs to the list of allocation resources
func IsQuotaType(resourceType ResourceType) bool {
	if _, ok := QuotaToComputeMap[resourceType]; ok {
		return true
	}
	return false
}

// Returns true if the given resource type argument belongs to the list of compute resources
func IsComputeType(resourceType ResourceType) bool {
	if _, ok := ComputeToQuotaMap[resourceType]; ok {
		return true
	}
	return false
}

func IsCPUType(resourceType ResourceType) bool {
	if _, ok := CPUResources[resourceType]; ok {
		return true
	}
	return false
}

type MetricProp string

const (
	Capacity MetricProp = "Capacity"
	Used     MetricProp = "Used"
)

type Metric interface {
	GetResourceType() ResourceType
	GetMetricProp() MetricProp
	GetUID() string
	GetValue() interface{}
	UpdateValue(existing interface{}, maxMetricPointsSize int) Metric
}

type MetricFilterFunc func(m Metric) bool

type ResourceMetric struct {
	resourceType ResourceType
	metricProp   MetricProp
	value        interface{}
}

type Points struct {
	Values []float64
	// Time at which the last metric value was appended
	Timestamp int64
}

func NewResourceMetric(rType ResourceType, mProp MetricProp, v interface{}) ResourceMetric {
	return ResourceMetric{
		resourceType: rType,
		metricProp:   mProp,
		value:        v,
	}
}

type EntityDetails struct {
	metricUID string

	entityType DiscoveredEntityType
	entityID   string
}

type EntityResourceMetric struct {
	EntityDetails
	ResourceMetric
}

func NewEntityResourceMetric(eType DiscoveredEntityType, id string, rType ResourceType, mProp MetricProp,
	v interface{}) EntityResourceMetric {
	return EntityResourceMetric{
		EntityDetails: EntityDetails{
			metricUID:  GenerateEntityResourceMetricUID(eType, id, rType, mProp),
			entityType: eType,
			entityID:   id,
		},
		ResourceMetric: NewResourceMetric(rType, mProp, v),
	}
}

func (m EntityResourceMetric) GetResourceType() ResourceType {
	return m.resourceType
}

func (m EntityResourceMetric) GetMetricProp() MetricProp {
	return m.metricProp
}

func (m EntityResourceMetric) GetUID() string {
	return m.metricUID
}

func (m EntityResourceMetric) GetValue() interface{} {
	return m.value
}

func (m EntityResourceMetric) UpdateValue(existing interface{}, maxMetricPointsSize int) Metric {
	typedExisting, isRightType := existing.(EntityResourceMetric)
	if !isRightType {
		glog.Warning("Skipping metrics value update as metrics type mismatches from cache.")
		return m
	}

	newPoints, isMultiPoint := m.value.(Points)
	if !isMultiPoint {
		m.value = typedExisting.value
		return m
	}

	// The caller should ensure that right type is created and matching
	// type updated for multi point metrics, else this will CRASH.
	points := typedExisting.value.(Points)
	points.Values = append(points.Values, newPoints.Values...)
	// If points length is larger than maxMetricPointsSize, use latest maxMetricPointsSize of points
	if len(points.Values) > maxMetricPointsSize {
		points.Values = points.Values[len(points.Values)-maxMetricPointsSize:]
	}
	points.Timestamp = newPoints.Timestamp
	m.value = points
	return m
}

// Generate the UID for each metric entry based on entityType, entityID, resourceType and metricType.
func GenerateEntityResourceMetricUID(eType DiscoveredEntityType, id string, rType ResourceType, mType MetricProp) string {
	return string(eType) + "-" + id + "-" + string(rType) + "-" + string(mType)
}

type EntityStateMetric struct {
	EntityDetails

	resourceType ResourceType
	value        interface{}
	metricProp   MetricProp
}

type StateMetric struct {
	resourceType ResourceType
	metricProp   MetricProp
	value        interface{}
}

func NewEntityStateMetric(eType DiscoveredEntityType, id string, rType ResourceType, v interface{}) EntityStateMetric {
	return EntityStateMetric{
		EntityDetails: EntityDetails{
			metricUID:  GenerateEntityStateMetricUID(eType, id, rType),
			entityType: eType,
			entityID:   id,
		},
		resourceType: rType,
		value:        v,
	}
}

func (m EntityStateMetric) GetResourceType() ResourceType {
	return m.resourceType
}

func (m EntityStateMetric) GetMetricProp() MetricProp {
	return m.metricProp
}

func (m EntityStateMetric) GetUID() string {
	return m.metricUID
}

func (m EntityStateMetric) GetValue() interface{} {
	return m.value
}

func (m EntityStateMetric) UpdateValue(existing interface{}, maxMetricPointsSize int) Metric {
	// NOP
	return nil
}

// Generate the UID for each metric entry based on entityType, entityID and resourceType.
func GenerateEntityStateMetricUID(eType DiscoveredEntityType, id string, rType ResourceType) string {
	return string(eType) + "-" + id + "-" + string(rType)
}
