package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
)

var TestEntities = []struct {
	entityType metrics.DiscoveredEntityType
	name       string
	uid        string
}{
	{metrics.NodeType, "node1", "node1"},
	{metrics.PodType, "pod1", "pod1"},
	{metrics.ContainerType, "container1", "container1"},
	{metrics.NamespaceType, "namespace1", "namespace1"},
}

var TestSoldResources = []struct {
	resourceType metrics.ResourceType
	capacity     float64
}{
	{metrics.CPU, 200},
	{metrics.Memory, 2},
	{metrics.MemoryLimitQuota, 1},
	{metrics.CPULimitQuota, 100},
}

var TestProvider = []struct {
	providerType   metrics.DiscoveredEntityType
	providerId     string
	boughtResource map[metrics.ResourceType]float64
}{
	{metrics.NodeType, "node1", map[metrics.ResourceType]float64{metrics.CPU: 500, metrics.Memory: 3}},
	{metrics.NodeType, "node2", map[metrics.ResourceType]float64{metrics.CPU: 500, metrics.Memory: 3}},
	{metrics.NodeType, "node3", map[metrics.ResourceType]float64{metrics.CPULimitQuota: 500}},
}

func TestKubeEntity(t *testing.T) {
	namespace := "ns1"
	cluster := "cluster1"
	for _, testEntity := range TestEntities {
		kubeEntity := NewKubeEntity(testEntity.entityType, cluster, namespace,
			testEntity.name, testEntity.uid)
		assert.Equal(t, kubeEntity.Namespace, namespace)
		assert.Equal(t, kubeEntity.ClusterName, cluster)
		assert.Equal(t, kubeEntity.Name, testEntity.name)
		assert.Equal(t, kubeEntity.UID, testEntity.uid)
	}
}

func TestKubeEntityAddResource(t *testing.T) {
	namespace := "ns1"
	cluster := "cluster1"
	var testEntity = struct {
		entityType metrics.DiscoveredEntityType
		name       string
		uid        string
	}{metrics.PodType, "pod1", "pod1"}

	kubeEntity := NewKubeEntity(testEntity.entityType, cluster, namespace,
		testEntity.name, testEntity.uid)
	for _, testResource := range TestSoldResources {
		kubeEntity.AddResource(testResource.resourceType, testResource.capacity, 0.0)
	}
	for _, testResource := range TestSoldResources {
		kubeResource, err := kubeEntity.GetResource(testResource.resourceType)
		assert.Nil(t, err)
		assert.Equal(t, kubeResource.Capacity, testResource.capacity)
		assert.Equal(t, kubeResource.Used, 0.0)
	}
}

func TestKubeEntityChangeResourceCapacity(t *testing.T) {
	namespace := "ns1"
	cluster := "cluster1"
	var testEntity = struct {
		entityType metrics.DiscoveredEntityType
		name       string
		uid        string
	}{metrics.PodType, "pod1", "pod1"}

	kubeEntity := NewKubeEntity(testEntity.entityType, cluster, namespace,
		testEntity.name, testEntity.uid)
	for _, testResource := range TestSoldResources {
		kubeEntity.AddResource(testResource.resourceType, testResource.capacity, 0.0)
	}
	for _, testResource := range TestSoldResources {
		_, err := kubeEntity.GetResource(testResource.resourceType)
		assert.Nil(t, err)
		cpuCap, _ := kubeEntity.GetResourceCapacity(testResource.resourceType)
		assert.Equal(t, testResource.capacity, cpuCap)

		kubeEntity.SetResourceCapacity(testResource.resourceType, testResource.capacity*2)
		cpuCap, _ = kubeEntity.GetResourceCapacity(testResource.resourceType)
		assert.Equal(t, testResource.capacity*2, cpuCap)
	}
}

func TestKubeEntityAddProvider(t *testing.T) {
	namespace := "ns1"
	cluster := "cluster1"
	var testEntity = struct {
		entityType metrics.DiscoveredEntityType
		name       string
		uid        string
	}{metrics.PodType, "pod1", "pod1"}

	kubeEntity := NewKubeEntity(testEntity.entityType, cluster, namespace,
		testEntity.name, testEntity.uid)

	for _, testProvider := range TestProvider {
		for resourceType, used := range testProvider.boughtResource {
			kubeEntity.AddProviderResource(testProvider.providerType,
				testProvider.providerId,
				resourceType, used)
		}
	}

	for _, testProvider := range TestProvider {
		for resourceType, resourceUsed := range testProvider.boughtResource {
			resource, err := kubeEntity.GetBoughtResource(testProvider.providerId, resourceType)
			assert.Nil(t, err)
			assert.Equal(t, resourceUsed, resource.Used)
		}
	}
}
