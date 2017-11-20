package repository

import (
	"fmt"
	"bytes"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
)

// Abstraction for a kubernetes cluster entity to represent in the Turbonomic server
type KubeEntity struct {
	EntityType          metrics.DiscoveredEntityType
	ClusterName         string
	Namespace           string
	Name                string
	UID                 string
	ComputeResources    map[metrics.ResourceType]*KubeDiscoveredResource // resources from the environment
	AllocationResources map[metrics.ResourceType]*KubeDiscoveredResource // resources from the environment
	ProviderMap         map[string]*KubeResourceProvider
}

// Creates a new entity for the given entity type and uid.
func NewKubeEntity(entityType metrics.DiscoveredEntityType,
clusterName, namespaceName, displayName, uid string) *KubeEntity {
	return &KubeEntity{
		EntityType:          entityType,
		ClusterName:         clusterName,
		Namespace:           namespaceName,
		Name:                displayName,
		UID:                 uid,
		ComputeResources:    make(map[metrics.ResourceType]*KubeDiscoveredResource),
		AllocationResources: make(map[metrics.ResourceType]*KubeDiscoveredResource),
		ProviderMap:         make(map[string]*KubeResourceProvider),
	}
}

func (entity *KubeEntity) String() string {
	var buffer bytes.Buffer
	var line string
	line = fmt.Sprintf("Type:%s %s::%s::%s::%s\n", entity.EntityType,
		entity.ClusterName, entity.Namespace, entity.Name, entity.UID)
	buffer.WriteString(line)
	for _, resource := range entity.AllocationResources {
		line := fmt.Sprintf("\tallocation resource:%s Capacity=%f, Used=%f\n",
			resource.Type, resource.Capacity, resource.Used)
		buffer.WriteString(line)
	}
	for _, resource := range entity.ComputeResources {
		line := fmt.Sprintf("\tcompute resource:%s Capacity=%f, Used=%f\n",
			resource.Type, resource.Capacity, resource.Used)
		buffer.WriteString(line)
	}
	for _, provider := range entity.ProviderMap {
		line := fmt.Sprintf("\tprovider:%s:%s\n", provider.EntityType, provider.UID)
		buffer.WriteString(line)
		for _, resource := range provider.BoughtCompute {
			line := fmt.Sprintf("\t\tcompute bought:%s Used=%f\n",
				resource.Type, resource.Used)
			buffer.WriteString(line)
		}
		for _, resource := range provider.BoughtAllocation {
			line := fmt.Sprintf("\t\tallocation bought:%s Used=%f\n",
				resource.Type, resource.Used)
			buffer.WriteString(line)
		}
	}
	return buffer.String()
}

func (kubeEntity *KubeEntity) GetComputeResource(resourceType metrics.ResourceType) (*KubeDiscoveredResource, error) {
	return GetResource(resourceType, kubeEntity.ComputeResources)
}

func (kubeEntity *KubeEntity) GetAllocationResource(resourceType metrics.ResourceType) (*KubeDiscoveredResource, error) {
	return GetResource(resourceType, kubeEntity.AllocationResources)
}

func (kubeEntity *KubeEntity) GetResourceCapacity(resourceType metrics.ResourceType) float64 {
	value, _ := GetResource(resourceType, kubeEntity.ComputeResources)
	return value.Capacity
}

func (kubeEntity *KubeEntity) GetResourceUsed(resourceType metrics.ResourceType) float64 {
	value, _ := GetResource(resourceType, kubeEntity.ComputeResources)
	return value.Used
}

// Abstraction for a kubernetes cluster resource to represent in the Turbonomic server
type KubeDiscoveredResource struct {
	Type     metrics.ResourceType
	Capacity float64
	Used     float64
}

// Abstraction for the provider of kubernetes cluster resource to represent in the Turbonomic server
type KubeResourceProvider struct {
	EntityType       metrics.DiscoveredEntityType
	UID              string
	BoughtCompute    map[metrics.ResourceType]*KubeBoughtResource
	BoughtAllocation map[metrics.ResourceType]*KubeBoughtResource
}

type KubeBoughtResource struct {
	Type        metrics.ResourceType
	Used        float64
	Reservation float64
}

// =================================================================================================

func GetResource(resourceType metrics.ResourceType,
resourceMap map[metrics.ResourceType]*KubeDiscoveredResource)(*KubeDiscoveredResource, error) {
	resource, exists := resourceMap[resourceType]
	if !exists {
		return nil, fmt.Errorf("%s missing\n", resourceType)
	}
	return resource, nil
}
