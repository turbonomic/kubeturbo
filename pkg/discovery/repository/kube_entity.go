package repository

import (
	"bytes"
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
)

// Abstraction for a kubernetes cluster entity to represent in the Turbonomic server supply chain
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

// Abstraction for a resource that the kubernetes entity will sell to its consumers
// in the Turbonomic server supply chain
type KubeDiscoveredResource struct {
	Type     metrics.ResourceType
	Capacity float64
	Used     float64
}

// Abstraction for the resource provider for a kubernetes entity
type KubeResourceProvider struct {
	EntityType       metrics.DiscoveredEntityType
	UID              string
	BoughtCompute    map[metrics.ResourceType]*KubeBoughtResource
	BoughtAllocation map[metrics.ResourceType]*KubeBoughtResource
}

// Abstraction for a resource that a kubernetes entity will be buy from its provider
// in the Turbonomic server supply chain
type KubeBoughtResource struct {
	Type        metrics.ResourceType
	Used        float64
	Reservation float64
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
	line = fmt.Sprintf("%s %s::%s\n", entity.EntityType, entity.Name, entity.UID)
	buffer.WriteString(line)
	line = fmt.Sprintf("Cluster:%s Namespace:%s\n", entity.ClusterName, entity.Namespace)
	buffer.WriteString(line)
	if len(entity.AllocationResources) > 0 {
		buffer.WriteString("Allocation resource:\n")
		for _, resource := range entity.AllocationResources {
			line := fmt.Sprintf("\t%s Capacity=%f, Used=%f\n",
				resource.Type, resource.Capacity, resource.Used)
			buffer.WriteString(line)
		}
	}
	if len(entity.ComputeResources) > 0 {
		buffer.WriteString("Compute resource:\n")
		for _, resource := range entity.ComputeResources {
			line := fmt.Sprintf("\t%s Capacity=%f, Used=%f\n",
				resource.Type, resource.Capacity, resource.Used)
			buffer.WriteString(line)
		}
	}
	for _, provider := range entity.ProviderMap {
		line := fmt.Sprintf("Provider:%s:%s\n", provider.EntityType, provider.UID)
		buffer.WriteString(line)
		if len(provider.BoughtCompute) > 0 {
			buffer.WriteString("\tCompute bought:\n")
			for _, resource := range provider.BoughtCompute {
				line := fmt.Sprintf("\t\t%s Used=%f\n", resource.Type, resource.Used)
				buffer.WriteString(line)
			}
		}
		if len(provider.BoughtAllocation) > 0 {
			buffer.WriteString("\tAllocation bought:\n")
			for _, resource := range provider.BoughtAllocation {
				line := fmt.Sprintf("\t\t%s Used=%f\n", resource.Type, resource.Used)
				buffer.WriteString(line)
			}
		}
	}
	return buffer.String()
}

func (kubeEntity *KubeEntity) AddResource(resourceType metrics.ResourceType,
	capValue, usedValue float64) {
	if metrics.IsComputeType(resourceType) {
		kubeEntity.AddComputeResource(resourceType, capValue, usedValue)
	} else if metrics.IsAllocationType(resourceType) {
		kubeEntity.AddAllocationResource(resourceType, capValue, usedValue)
	}
}

func (kubeEntity *KubeEntity) GetResource(resourceType metrics.ResourceType,
) (*KubeDiscoveredResource, error) {
	if metrics.IsComputeType(resourceType) {
		return kubeEntity.GetComputeResource(resourceType)
	} else if metrics.IsAllocationType(resourceType) {
		return kubeEntity.GetAllocationResource(resourceType)
	}
	return nil, fmt.Errorf("%s: invalid resource %s\n", kubeEntity.Name, resourceType)
}

func (kubeEntity *KubeEntity) SetResourceCapacity(resourceType metrics.ResourceType,
	computeCap float64) error {
	resource, err := kubeEntity.GetResource(resourceType)
	if err != nil {
		return err
	}
	resource.Capacity = computeCap
	return nil
}

func (kubeEntity *KubeEntity) SetResourceUsed(resourceType metrics.ResourceType,
	computeUsed float64) error {
	resource, err := kubeEntity.GetResource(resourceType)
	if err != nil {
		return err
	}
	resource.Used = computeUsed
	return nil
}

// Create the KubeDiscoveredResource for the given compute resource type
func (kubeEntity *KubeEntity) AddComputeResource(resourceType metrics.ResourceType,
	computeCap, computeUsed float64) {
	r := &KubeDiscoveredResource{
		Type:     resourceType,
		Capacity: computeCap,
	}
	kubeEntity.ComputeResources[resourceType] = r
}

// Create the KubeDiscoveredResource for the given allocation resource type
func (kubeEntity *KubeEntity) AddAllocationResource(resourceType metrics.ResourceType,
	capValue, usedValue float64) {
	r := &KubeDiscoveredResource{
		Type:     resourceType,
		Capacity: capValue,
		Used:     usedValue,
	}
	kubeEntity.AllocationResources[resourceType] = r
}

func (kubeEntity *KubeEntity) GetComputeResource(resourceType metrics.ResourceType,
) (*KubeDiscoveredResource, error) {
	return getResource(resourceType, kubeEntity.ComputeResources)
}

func (kubeEntity *KubeEntity) GetAllocationResource(resourceType metrics.ResourceType,
) (*KubeDiscoveredResource, error) {
	return getResource(resourceType, kubeEntity.AllocationResources)
}

func (kubeEntity *KubeEntity) GetResourceCapacity(resourceType metrics.ResourceType) (float64, error) {
	value, err := kubeEntity.GetResource(resourceType)
	if err != nil {
		return 0.0, err
	}
	return value.Capacity, nil
}

func (kubeEntity *KubeEntity) GetResourceUsed(resourceType metrics.ResourceType) (float64, error) {
	value, err := kubeEntity.GetResource(resourceType)
	if err != nil {
		return 0.0, err
	}
	return value.Used, nil
}

func (kubeEntity *KubeEntity) AddProviderResource(providerType metrics.DiscoveredEntityType,
	providerId string,
	resourceType metrics.ResourceType,
	usedValue float64) {
	_, exists := kubeEntity.ProviderMap[providerId]
	if !exists {
		provider := &KubeResourceProvider{
			EntityType:       providerType,
			UID:              providerId,
			BoughtCompute:    make(map[metrics.ResourceType]*KubeBoughtResource),
			BoughtAllocation: make(map[metrics.ResourceType]*KubeBoughtResource),
		}
		kubeEntity.ProviderMap[providerId] = provider
	}

	provider, _ := kubeEntity.ProviderMap[providerId]
	kubeResource := &KubeBoughtResource{
		Type: resourceType,
		Used: usedValue,
	}
	if metrics.IsComputeType(resourceType) {
		provider.BoughtCompute[resourceType] = kubeResource
	} else if metrics.IsAllocationType(resourceType) {
		provider.BoughtAllocation[resourceType] = kubeResource
	}
}

func (kubeEntity *KubeEntity) GetProvider(providerId string) *KubeResourceProvider {
	return kubeEntity.ProviderMap[providerId]
}

func (kubeEntity *KubeEntity) GetBoughtResource(providerId string,
	resourceType metrics.ResourceType,
) (*KubeBoughtResource, error) {
	provider, exists := kubeEntity.ProviderMap[providerId]
	if !exists {
		return nil, fmt.Errorf("%s missing\n", resourceType)
	}
	var resourceMap map[metrics.ResourceType]*KubeBoughtResource
	if metrics.IsComputeType(resourceType) {
		resourceMap = provider.BoughtCompute
	} else if metrics.IsAllocationType(resourceType) {
		resourceMap = provider.BoughtAllocation
	}
	resource, exists := resourceMap[resourceType]
	if !exists {
		return nil, fmt.Errorf("%s missing\n", resourceType)
	}
	return resource, nil
}

func getResource(resourceType metrics.ResourceType,
	resourceMap map[metrics.ResourceType]*KubeDiscoveredResource,
) (*KubeDiscoveredResource, error) {
	resource, exists := resourceMap[resourceType]
	if !exists {
		return nil, fmt.Errorf("%s missing\n", resourceType)
	}
	return resource, nil
}
