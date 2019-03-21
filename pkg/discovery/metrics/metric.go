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
	CPU               ResourceType = "CPU"
	Memory            ResourceType = "Memory"
	CPULimit          ResourceType = "CPULimit"
	MemoryLimit       ResourceType = "MemoryLimit"
	CPURequest        ResourceType = "CPURequest"
	MemoryRequest     ResourceType = "MemoryRequest"
	CPUProvisioned    ResourceType = "CPUProvisioned"
	MemoryProvisioned ResourceType = "MemoryProvisioned"
	Transaction       ResourceType = "Transaction"
	ObjectCount       ResourceType = "ObjectCount"

	Access       ResourceType = "Access"
	Cluster      ResourceType = "Cluster"
	Schedulable  ResourceType = "Schedulable"
	CpuFrequency ResourceType = "CpuFrequency"
	Owner        ResourceType = "Owner"
	OwnerType    ResourceType = "OwnerType"
)

var (
	// Mapping of Kubernetes API Server resource names to the compute resource types
	KubeComputeResourceTypes = map[v1.ResourceName]ResourceType{
		v1.ResourceCPU:    CPU,
		v1.ResourceMemory: Memory,
	}

	// Mapping of Kubernetes API Server resource names to the allocation resource types
	KubeAllocatonResourceTypes = map[v1.ResourceName]ResourceType{
		v1.ResourceLimitsCPU:      CPULimit,
		v1.ResourceLimitsMemory:   MemoryLimit,
		v1.ResourceRequestsCPU:    CPURequest,
		v1.ResourceRequestsMemory: MemoryRequest,
	}

	ReverseKubeResourceTypes = map[ResourceType]v1.ResourceName{
		CPULimit:      v1.ResourceLimitsCPU,
		MemoryLimit:   v1.ResourceLimitsMemory,
		CPURequest:    v1.ResourceRequestsCPU,
		MemoryRequest: v1.ResourceRequestsMemory,
		ObjectCount:   v1.ResourcePods,
		CPU:           v1.ResourceCPU,
		Memory:        v1.ResourceMemory,
	}

	// List of Compute resources
	ComputeResources = []ResourceType{CPU, Memory}

	// List of Allocation resources
	ComputeAllocationResources = []ResourceType{CPULimit, MemoryLimit}

	// List of cpu related metrics
	CPUResources = []ResourceType{CPULimit, CPURequest, CPU}

	// Mapping of allocation to compute resources
	AllocationToComputeMap = map[ResourceType]ResourceType{
		CPULimit:      CPU,
		MemoryLimit:   Memory,
		CPURequest:    CPU,
		MemoryRequest: Memory,
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
