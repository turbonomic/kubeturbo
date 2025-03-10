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
	VolumeType      DiscoveredEntityType = "Volume"
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
	StorageAmount      ResourceType = "StorageAmount"
	VCPUThrottling     ResourceType = "VCPUThrottling"

	Access              ResourceType = "Access"
	Cluster             ResourceType = "Cluster"
	CpuFrequency        ResourceType = "CpuFrequency"
	Owner               ResourceType = "Owner"
	OwnerType           ResourceType = "OwnerType"
	OwnerUID            ResourceType = "OwnerUID"
	IsInjectedSidecar   ResourceType = "IsInjectedSidecar"
	MetricsAvailability ResourceType = "MetricsAvailability"
)

var (
	// KubeComputeResourceTypes is a mapping of Kubernetes API Server resource names to the compute resource types
	KubeComputeResourceTypes = map[v1.ResourceName][]ResourceType{
		v1.ResourceCPU:    {CPU, CPURequest},
		v1.ResourceMemory: {Memory, MemoryRequest},
	}

	// KubeQuotaResourceTypes is a mapping of Kubernetes API Server resource names to the allocation resource types
	// A resource quota definition, although undocumented, honours both "cpu" and "requests.cpu"
	// as keys for cpu request quota and likewise for memory.
	// This has more to do with legacy support, where when the resourcequota proposal was introduced
	// had support only for "cpu" and "memory" corresponding to limiting only requests.
	KubeQuotaResourceTypes = map[v1.ResourceName]ResourceType{
		v1.ResourceCPU:            CPURequestQuota,
		v1.ResourceMemory:         MemoryRequestQuota,
		v1.ResourceLimitsCPU:      CPULimitQuota,
		v1.ResourceLimitsMemory:   MemoryLimitQuota,
		v1.ResourceRequestsCPU:    CPURequestQuota,
		v1.ResourceRequestsMemory: MemoryRequestQuota,
	}

	// QuotaToComputeMap is a mapping of quota to compute resources
	QuotaToComputeMap = map[ResourceType]ResourceType{
		CPULimitQuota:      CPU,
		MemoryLimitQuota:   Memory,
		CPURequestQuota:    CPURequest,
		MemoryRequestQuota: MemoryRequest,
	}

	// QuotaLimitTypes is a set of resource limit quota types
	QuotaLimitTypes = map[ResourceType]struct{}{
		CPULimitQuota:    {},
		MemoryLimitQuota: {},
	}

	// QuotaRequestTypes is a set of resource request quota types
	QuotaRequestTypes = map[ResourceType]struct{}{
		CPURequestQuota:    {},
		MemoryRequestQuota: {},
	}

	// PointsResources is a list of resources that we collect multiple metric points per discovery cycle
	PointsResources = []ResourceType{CPU, Memory}

	// ComputeResources is a list of Compute resources
	ComputeResources []ResourceType

	// QuotaResources is a list of Quota resources
	QuotaResources []ResourceType

	// CPUResources is a list of cpu related metrics
	CPUResources map[ResourceType]bool

	// ComputeToQuotaMap is a mapping of compute to quota resources
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
		QuotaResources = appendUnique(QuotaResources, resource)
		if name == v1.ResourceCPU || name == v1.ResourceLimitsCPU || name == v1.ResourceRequestsCPU {
			cpuResources = appendUnique(cpuResources, resource)
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

func appendUnique(resourceTypes []ResourceType, resourceType ResourceType) []ResourceType {
	for _, rt := range resourceTypes {
		if rt == resourceType {
			// This type is already in the slice, don't add
			return resourceTypes
		}
	}

	return append(resourceTypes, resourceType)
}

// IsQuotaType returns true if the given resource type argument belongs to the list of allocation resources
func IsQuotaType(resourceType ResourceType) bool {
	if _, ok := QuotaToComputeMap[resourceType]; ok {
		return true
	}
	return false
}

// IsComputeType returns true if the given resource type argument belongs to the list of compute resources
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
	Capacity  MetricProp = "Capacity"
	Used      MetricProp = "Used"
	Available MetricProp = "Available"
	Threshold MetricProp = "Threshold"
)

type Metric interface {
	GetResourceType() ResourceType
	GetMetricProp() MetricProp
	GetUID() string
	GetValue() interface{}
	UpdateValue(existing interface{}, maxMetricPointsSize int) Metric
}

type MetricValue struct {
	// Average of all data points
	Avg float64
	// Peak of all data points
	Peak float64
}

type MetricFilterFunc func(m Metric) bool

type ResourceMetric struct {
	resourceType ResourceType
	metricProp   MetricProp
	value        interface{}
}

// Point is a data point of a resource metric sample collected from kubelet
type Point struct {
	// Resource metric value
	Value float64
	// Time at which the metric value is collected
	Timestamp int64
}

// ThrottlingCumulative is a counter type cumulative data point of cpu throttling metric sample collected from kubelet
type ThrottlingCumulative struct {
	// Cumulative throttled number of runnable periods for the resource
	Throttled float64
	// Cumulative total number of runnable periods for the resource
	Total float64
	// Container CPU limit when collecting this set of throttling metrics
	CPULimit float64
	// Time at which the metric value is collected
	Timestamp int64
	// Cumulative throttled time during the runnable periods for the resource
	ThrottledTime float64
	// Cumulative total usage during the runnable periods for the resource
	TotalUsage float64
	// Container Threads
	ContainerThreads float64
}

// Cumulative is a counter type cumulative data. The difference between Cumulative and Point type is that the former
// is cumulative data and the latter is instant data. For example, the UsageCoreNanoSeconds is a Cumulative type, while
// the UsageNanoCores is a Point type.
type Cumulative struct {
	// Resource metric value
	Value float64
	// Time at which the metric value is collected
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

// UpdateValue aggregates the new metric with existing metric in the metric sink
func (m EntityResourceMetric) UpdateValue(existing interface{}, maxMetricPointsSize int) Metric {
	existingResourceMetric, isRightType := existing.(EntityResourceMetric)
	if !isRightType {
		glog.Warning("Skipping metrics value update as metrics type mismatches from cache.")
		return m
	}
	switch newValues := m.value.(type) {
	case []Point:
		// The caller should ensure that right type is created and matching
		// type updated, else this will CRASH.
		if existingValues, ok := existingResourceMetric.value.([]Point); !ok {
			// This should never happen, log for debugging purposes, and to avoid crash
			glog.Warningf("Skip metric value update for %v. The existing metric samples %v does not"+
				" have the same []Point type as the new metric sample %v.",
				existingResourceMetric.GetUID(), existingValues, newValues)
			return m
		}
		points := existingResourceMetric.value.([]Point)
		points = append(points, newValues...)
		// If points length is larger than maxMetricPointsSize, use latest maxMetricPointsSize of points
		if len(points) > maxMetricPointsSize {
			points = points[len(points)-maxMetricPointsSize:]
		}
		m.value = points
	case []Cumulative:
		if existingValues, ok := existingResourceMetric.value.([]Cumulative); !ok {
			// This should never happen, log for debugging purposes, and to avoid crash
			glog.Warningf("Skip metric value update for %v. The existing metric samples %v does not"+
				" have the same []Cumulative type as the new metric sample %v.",
				existingResourceMetric.GetUID(), existingValues, newValues)
			return m
		}
		cu := existingResourceMetric.value.([]Cumulative)
		cu = append(cu, newValues...)
		// If points length is larger than maxMetricPointsSize, use latest maxMetricPointsSize of points
		// For cumulative data points, we need maxMetricPointsSize+1 samples to compute maxMetricPointsSize metric data
		maxMetricPointsSize++
		if len(cu) > maxMetricPointsSize {
			cu = cu[len(cu)-maxMetricPointsSize:]
		}
		m.value = cu
	case []ThrottlingCumulative:
		// The caller should ensure that right type is created and matching type updated, else this will CRASH.
		tc := existingResourceMetric.value.([]ThrottlingCumulative)
		tc = append(tc, newValues...)
		// If points length is larger than maxMetricPointsSize, use latest maxMetricPointsSize of points
		// For cumulative data points, we need maxMetricPointsSize+1 samples to compute maxMetricPointsSize metric data
		maxMetricPointsSize++
		if len(tc) > maxMetricPointsSize {
			tc = tc[len(tc)-maxMetricPointsSize:]
		}
		m.value = tc
	default:
		m.value = existingResourceMetric.value
		return m
	}

	return m
}

// GenerateEntityResourceMetricUID generates the UID for each metric entry based on entityType, entityID, resourceType and metricType.
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
	return m
}

// GenerateEntityStateMetricUID generates the UID for each metric entry based on entityType, entityID and resourceType.
func GenerateEntityStateMetricUID(eType DiscoveredEntityType, id string, rType ResourceType) string {
	return string(eType) + "-" + id + "-" + string(rType)
}
