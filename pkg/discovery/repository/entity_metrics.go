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
}

func NewPodMetrics(podName, namespace, nodeName string) *PodMetrics {
	return &PodMetrics{
		PodName:       podName,
		Namespace:     namespace,
		NodeName:      nodeName,
		QuotaUsed:     make(map[metrics.ResourceType]float64),
		QuotaCapacity: make(map[metrics.ResourceType]float64),
	}
}

// Collection of quota resources sold by a namespace entity
type NamespaceMetrics struct {
	Namespace string
	// Amount of quota resources used by the pods running in the namespace
	QuotaSoldUsed map[metrics.ResourceType]float64
}

func NewNamespaceMetrics(namespace string) *NamespaceMetrics {
	return &NamespaceMetrics{
		Namespace:     namespace,
		QuotaSoldUsed: make(map[metrics.ResourceType]float64),
	}
}

func CreateDefaultNamespaceMetrics(namespace string) *NamespaceMetrics {
	namespaceMetrics := NewNamespaceMetrics(namespace)
	// quotas sold
	for _, allocationResource := range metrics.QuotaResources {
		namespaceMetrics.QuotaSoldUsed[allocationResource] = 0.0
	}
	return namespaceMetrics
}

func (namespaceMetrics *NamespaceMetrics) UpdateQuotaSoldUsed(quotaSoldUsed map[metrics.ResourceType]float64) {
	if namespaceMetrics.QuotaSoldUsed == nil {
		namespaceMetrics.QuotaSoldUsed = make(map[metrics.ResourceType]float64)
	}
	for resourceType, used := range quotaSoldUsed {
		var totalUsed float64
		currentUsed, exists := namespaceMetrics.QuotaSoldUsed[resourceType]
		if !exists {
			totalUsed = used
		} else {
			totalUsed = currentUsed + used
		}
		namespaceMetrics.QuotaSoldUsed[resourceType] = totalUsed
	}
}
