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
	// Mapping of Kubernetes API Server resource names to the compute resource types
	KubeComputeResourceTypes = map[v1.ResourceName][]ResourceType{
		v1.ResourceCPU:    {CPU, CPURequest},
		v1.ResourceMemory: {Memory, MemoryRequest},
	}

	// Mapping of Kubernetes API Server resource names to the allocation resource types
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

	// List of resources that we collect mulitple metric points per discovery cycle
	PointsResources = []ResourceType{CPU, Memory}

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

// Data point of a resource metric sample collected from kubelet
type Point struct {
	// Resource metric value
	Value float64
	// Time at which the metric value is collected
	Timestamp int64
}

// Counter type cumilative data point of cpu throttling metric sample collected from kubelet
type ThrottlingCumulative struct {
	// Cumulative throttled number of runnable periods for the resource
	Throttled float64
	// Cumulative total number of runnable periods for the resource
	Total float64
	// Container CPU limit when collecting this set of throttling metrics
	CPULimit float64
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

func (m EntityResourceMetric) UpdateValue(existing interface{}, maxMetricPointsSize int) Metric {
	typedExisting, isRightType := existing.(EntityResourceMetric)
	if !isRightType {
		glog.Warning("Skipping metrics value update as metrics type mismatches from cache.")
		return m
	}

	switch newValues := m.value.(type) {
	case []Point:
		// The caller should ensure that right type is created and matching
		// type updated, else this will CRASH.
		points := typedExisting.value.([]Point)
		points = append(points, newValues...)
		// If points length is larger than maxMetricPointsSize, use latest maxMetricPointsSize of points
		if len(points) > maxMetricPointsSize {
			points = points[len(points)-maxMetricPointsSize:]
		}
		m.value = points
	case []ThrottlingCumulative:
		// The caller should ensure that right type is created and matching
		// type updated, else this will CRASH.
		tc := typedExisting.value.([]ThrottlingCumulative)
		tc = append(tc, newValues...)
		if len(tc) > maxMetricPointsSize {
			tc = tc[len(tc)-maxMetricPointsSize:]
		}
		m.value = tc
	default:
		m.value = typedExisting.value
		return m
	}

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
	return m
}

// Generate the UID for each metric entry based on entityType, entityID and resourceType.
func GenerateEntityStateMetricUID(eType DiscoveredEntityType, id string, rType ResourceType) string {
	return string(eType) + "-" + id + "-" + string(rType)
}

// AccumulateMultiPoints adds the metric points from the "newPortion" to the currently "accumulated".
// Since the length of the metric point slice may be different due to pod startup and shutdown in the collection period,
// this is not a trivial addition.  For now we will just discard any slice of metric points not covering the full
// collection period.
func AccumulateMultiPoints(accumulated map[ResourceType][]Point, newPortion map[ResourceType][]Point) {
	for resourceType, podUsed := range newPortion {
		existingPoints := accumulated[resourceType]
		if len(existingPoints) < len(podUsed) {
			// Throw away existing points with a shorter length, which mostly indicates that it is from pods with
			// readings for a partial collection period (just started or shut down).  We should do a better job to
			// aggregate this kind of partial readings based on timestamps, perhaps with the knowledge of the sampling
			// periods.  But for the time being I'm just discarding them. TODO
			existingPoints = make([]Point, len(podUsed))
			copy(existingPoints, podUsed)
			accumulated[resourceType] = existingPoints
		} else if len(podUsed) < len(existingPoints) {
			// Similarly discard for now as these are readings from a partial collection period
		} else {
			for idx, point := range podUsed {
				existingPoints[idx].Value += point.Value
			}
		}
	}
}
