package metrics

import (
	v1 "k8s.io/api/core/v1"
)

type DiscoveredEntityType string

type ResourceType string

const (
	ClusterType     DiscoveredEntityType = "Cluster"
	QuotaType       DiscoveredEntityType = "Quota"
	NodeType        DiscoveredEntityType = "Node"
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
	CPUQuota           ResourceType = "CPUQuota"
	MemoryQuota        ResourceType = "MemoryQuota"
	CPURequestQuota    ResourceType = "CPURequestQuota"
	MemoryRequestQuota ResourceType = "MemoryRequestQuota"
	CPUProvisioned     ResourceType = "CPUProvisioned"
	MemoryProvisioned  ResourceType = "MemoryProvisioned"
	Transaction        ResourceType = "Transaction"

	Access       ResourceType = "Access"
	Cluster      ResourceType = "Cluster"
	CpuFrequency ResourceType = "CpuFrequency"
	Owner        ResourceType = "Owner"
	OwnerType    ResourceType = "OwnerType"
)

var (
	// Mapping of Kubernetes API Server resource names to the compute resource types
	KubeComputeResourceTypes = map[v1.ResourceName][]ResourceType{
		v1.ResourceCPU:    {CPU, CPURequest},
		v1.ResourceMemory: {Memory, MemoryRequest},
	}

	// Mapping of Kubernetes API Server resource names to the allocation resource types
	KubeAllocatonResourceTypes = map[v1.ResourceName]ResourceType{
		v1.ResourceLimitsCPU:      CPUQuota,
		v1.ResourceLimitsMemory:   MemoryQuota,
		v1.ResourceRequestsCPU:    CPURequestQuota,
		v1.ResourceRequestsMemory: MemoryRequestQuota,
	}

	// List of Compute resources
	ComputeResources = []ResourceType{
		CPU, Memory, CPURequest, MemoryRequest}

	// List of Allocation resources
	ComputeAllocationResources = []ResourceType{
		CPUQuota, MemoryQuota, CPURequestQuota, MemoryRequestQuota}

	// List of cpu related metrics
	CPUResources = []ResourceType{CPU, CPURequest, CPUQuota, CPURequestQuota}

	// Mapping of allocation to compute resources
	AllocationToComputeMap = map[ResourceType]ResourceType{
		CPUQuota:           CPU,
		MemoryQuota:        Memory,
		CPURequestQuota:    CPURequest,
		MemoryRequestQuota: MemoryRequest,
	}

	// Mapping of compute to allocation resources
	ComputeToAllocationMap = map[ResourceType]ResourceType{
		CPU:           CPUQuota,
		Memory:        MemoryQuota,
		CPURequest:    CPURequestQuota,
		MemoryRequest: MemoryRequestQuota,
	}
)

// Returns true if the given resource type argument belongs to the list of allocation resources
func IsAllocationType(resourceType ResourceType) bool {
	for _, rt := range ComputeAllocationResources {
		if rt == resourceType {
			return true
		}
	}
	return false
}

// Returns true if the given resource type argument belongs to the list of compute resources
func IsComputeType(resourceType ResourceType) bool {
	for _, rt := range ComputeResources {
		if rt == resourceType {
			return true
		}
	}
	return false
}

func IsCPUType(resourceType ResourceType) bool {
	for _, rt := range CPUResources {
		if rt == resourceType {
			return true
		}
	}
	return false
}

type MetricProp string

const (
	Capacity    MetricProp = "Capacity"
	Used        MetricProp = "Used"
	Reservation MetricProp = "Reservation"
)

type Metric interface {
	GetResourceType() ResourceType
	GetMetricProp() MetricProp
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

	entityType DiscoveredEntityType
	entityID   string

	ResourceMetric
}

func NewEntityResourceMetric(eType DiscoveredEntityType, id string, rType ResourceType, mProp MetricProp,
	v float64) EntityResourceMetric {
	return EntityResourceMetric{
		metricUID:      GenerateEntityResourceMetricUID(eType, id, rType, mProp),
		entityType:     eType,
		entityID:       id,
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

// Generate the UID for each metric entry based on entityType, entityID, resourceType and metricType.
func GenerateEntityResourceMetricUID(eType DiscoveredEntityType, id string, rType ResourceType, mType MetricProp) string {
	return string(eType) + "-" + id + "-" + string(rType) + "-" + string(mType)
}

type EntityStateMetric struct {
	metricUID string

	entityType DiscoveredEntityType
	entityID   string

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
		metricUID:    GenerateEntityStateMetricUID(eType, id, rType),
		entityType:   eType,
		entityID:     id,
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

// Generate the UID for each metric entry based on entityType, entityID and resourceType.
func GenerateEntityStateMetricUID(eType DiscoveredEntityType, id string, rType ResourceType) string {
	return string(eType) + "-" + id + "-" + string(rType)
}
