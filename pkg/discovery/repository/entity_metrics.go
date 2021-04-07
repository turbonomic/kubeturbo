package repository

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
)

// Collection of allocation resources bought by a pod
type PodMetrics struct {
	PodName   string
	Namespace string
	NodeName  string
	PodKey    string
	// Quota resources used and capacity
	QuotaUsed     map[metrics.ResourceType]float64
	QuotaCapacity map[metrics.ResourceType]float64
	// Actual used
	Used map[metrics.ResourceType][]metrics.Point
}

func NewPodMetrics(podName, namespace, nodeName string) *PodMetrics {
	return &PodMetrics{
		PodName:       podName,
		Namespace:     namespace,
		NodeName:      nodeName,
		QuotaUsed:     make(map[metrics.ResourceType]float64),
		QuotaCapacity: make(map[metrics.ResourceType]float64),
		Used:          make(map[metrics.ResourceType][]metrics.Point),
	}
}

// Collection of quota resources sold by a namespace entity
type NamespaceMetrics struct {
	Namespace string
	// Amount of quota resources used by the pods running in the namespace
	QuotaUsed map[metrics.ResourceType]float64
	// Amount of actual resources used by the pods running in the namespace
	Used map[metrics.ResourceType][]metrics.Point
}

func NewNamespaceMetrics(namespace string) *NamespaceMetrics {
	return &NamespaceMetrics{
		Namespace: namespace,
		QuotaUsed: make(map[metrics.ResourceType]float64),
		Used:      make(map[metrics.ResourceType][]metrics.Point),
	}
}

func CreateDefaultNamespaceMetrics(namespace string) *NamespaceMetrics {
	namespaceMetrics := NewNamespaceMetrics(namespace)
	// quotas used
	for _, allocationResource := range metrics.QuotaResources {
		namespaceMetrics.QuotaUsed[allocationResource] = 0.0
	}
	// actual used
	for _, resource := range metrics.ComputeResources {
		namespaceMetrics.Used[resource] = []metrics.Point{}
	}
	return namespaceMetrics
}

// AggregateQuotaUsed accumulates quota used for the namespace given a new used from a subset of pods in the namespace.
// Quota used is a single float64
func (namespaceMetrics *NamespaceMetrics) AggregateQuotaUsed(partialQuotaUsed map[metrics.ResourceType]float64) {
	if namespaceMetrics.QuotaUsed == nil {
		namespaceMetrics.QuotaUsed = make(map[metrics.ResourceType]float64)
	}
	for resourceType, podUsed := range partialQuotaUsed {
		namespaceMetrics.QuotaUsed[resourceType] += podUsed
	}
}

// AggregateUsed accumulates actual used for the namespace given a new used from a subset of pods in the namespace.
// Actual used is a slice of metrics.Point.
func (namespaceMetrics *NamespaceMetrics) AggregateUsed(partialUsed map[metrics.ResourceType][]metrics.Point) {
	if namespaceMetrics.Used == nil {
		namespaceMetrics.Used = make(map[metrics.ResourceType][]metrics.Point)
	}
	metrics.AccumulateMultiPoints(namespaceMetrics.Used, partialUsed)
}

// ContainerMetrics collects resource capacity and multiple usage data samples for container replicas which belong to the
// same ContainerSpec.
type ContainerMetrics struct {
	Capacity []float64
	// Used could be []metrics.Point or [][]metrics.ThrottlingCumulative
	// [][]metrics. ThrottlingCumulative stores multipoints segregated per container.
	// Storing it this way helps in identifying metrics per container while aggregating
	// the metrics for containerSpecs.
	// TODO: At some point this can be converted to a typed UsedMetric interface which
	// implements the methods like len(), append() and getPoints() and have the implementation
	// hidden from the users of this metric.
	Used interface{}
}

func NewContainerMetrics(capacity []float64, used interface{}) *ContainerMetrics {
	return &ContainerMetrics{
		Capacity: capacity,
		Used:     used,
	}
}

// ContainerSpecMetrics collects the shared portion of individual container replicas defined by the controller that manages
// the pods where these containers run, including container replicas and resource metrics with multiple samples of usage data.
type ContainerSpecMetrics struct {
	Namespace         string
	ControllerUID     string
	ContainerSpecName string
	ContainerSpecId   string
	// Container replicas number
	ContainerReplicas int32
	// Map from resource type to ContainerMetrics with multiple samples of resource usage data discovered from all
	// container replicas which belong to the same ContainerSpec.
	ContainerMetrics map[metrics.ResourceType]*ContainerMetrics
}

func NewContainerSpecMetrics(namespace, controllerUID, containerName, containerSpecId string) *ContainerSpecMetrics {
	return &ContainerSpecMetrics{
		Namespace:         namespace,
		ControllerUID:     controllerUID,
		ContainerSpecName: containerName,
		ContainerSpecId:   containerSpecId,
		ContainerReplicas: 1,
		ContainerMetrics:  make(map[metrics.ResourceType]*ContainerMetrics),
	}
}
