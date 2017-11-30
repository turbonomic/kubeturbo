package repository

import (
	"testing"
	"k8s.io/client-go/pkg/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/resource"
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
)

var TestNodes = []struct {
	name string
	cpuCap float64
	memCap float64
	cluster string
}{
	{"node1", 4.0, 819200, "cluster1"},
	{"node2", 5.0, 614400, "cluster1"},
	{"node3", 6.0, 409600, "cluster1"},
}

func TestKubeNode(t *testing.T) {
	for _, testNode := range TestNodes {
		resourceList := v1.ResourceList{
			v1.ResourceCPU: resource.MustParse(fmt.Sprint(testNode.cpuCap)),
			v1.ResourceMemory: resource.MustParse(fmt.Sprint(testNode.memCap)),
		}

		n1 := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNode.name,
				UID: types.UID(testNode.name),
			},
			Status: v1.NodeStatus{
				Allocatable: resourceList,
			},
		}

		kubenode := NewKubeNode(n1, testNode.cluster)
		fmt.Printf("%s\n", kubenode.KubeEntity)

		resource, _ := kubenode.GetComputeResource(metrics.CPU)
		assert.Equal(t,  resource.Capacity, testNode.cpuCap)
		resource, _ = kubenode.GetComputeResource(metrics.Memory)
		assert.Equal(t, resource.Capacity, testNode.memCap/1024)

		resource, _ = kubenode.GetComputeResource(metrics.CPULimit)
		assert.Nil(t,  resource)
		resource, _ = kubenode.GetComputeResource(metrics.MemoryLimit)
		assert.Nil(t,  resource)

		resource, _ = kubenode.GetAllocationResource(metrics.CPU)
		assert.Nil(t,  resource)
		resource, _ = kubenode.GetAllocationResource(metrics.Memory)
		assert.Nil(t,  resource)
	}
}


var TestQuotas = []struct {
	name string
	cpuLimit string
	cpuUsed string
	memLimit string
	memUsed string
}{
	{"quota1", "4", "3", "8Gi", "5Gi"},
	{"quota2", "0.5", "0.3", "7Mi", "5Gi"},
	{"quota3", "6000m", "300m", "6Ki", "5Ki"},
}

var TestNodeProviders = []struct {
	node string
	cpuUsed float64
	memUsed float64
} {
	{"node1", 1.0, 100*1024*1024.0},
	{"node2", 0.5, 150*1024.0*1024.0},
	{"node3", 1.5, 250*1024.0*1024.0},
}

func TestKubeQuota(t *testing.T) {
	namespace := "ns1"
	cluster := "cluster1"

	var clusterResources map[metrics.ResourceType]*KubeDiscoveredResource
	for _, testQuota := range TestQuotas {
		kubeQuota := CreateDefaultQuota(cluster, namespace, clusterResources)
		hardResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU: resource.MustParse(testQuota.cpuLimit),
			v1.ResourceLimitsMemory: resource.MustParse(testQuota.memLimit),
		}
		usedResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU: resource.MustParse(testQuota.cpuUsed),
			v1.ResourceLimitsMemory: resource.MustParse(testQuota.memUsed),
		}

		quota := &v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: testQuota.name,
				UID: types.UID(testQuota.name),
				Namespace: namespace,
			},
			Status: v1.ResourceQuotaStatus{
				Hard: hardResourceList,
				Used: usedResourceList,
			},
		}
		var quotaList []*v1.ResourceQuota
		quotaList = append(quotaList, quota)

		kubeQuota.ReconcileQuotas(quotaList)
		fmt.Printf("%s\n", kubeQuota.KubeEntity)

		resource, _ := kubeQuota.GetAllocationResource(metrics.CPULimit)
		quantity := hardResourceList[v1.ResourceLimitsCPU]
		cpuMilliCore := quantity.MilliValue()
		cpuCore := float64(cpuMilliCore) / util.MilliToUnit
		assert.Equal(t,  resource.Capacity, cpuCore)

		quantity = usedResourceList[v1.ResourceLimitsCPU]
		cpuMilliCore = quantity.MilliValue()
		cpuCore = float64(cpuMilliCore) / util.MilliToUnit
		assert.Equal(t,  resource.Used, cpuCore)

		resource, _ = kubeQuota.GetAllocationResource(metrics.MemoryLimit)
		quantity = hardResourceList[v1.ResourceLimitsMemory]
		memoryBytes := quantity.Value()
		memoryKiloBytes := float64(memoryBytes) / util.KilobytesToBytes
		assert.Equal(t, resource.Capacity, memoryKiloBytes)	// the least of the 3 quotas
		quantity = usedResourceList[v1.ResourceLimitsMemory]
		memoryBytes = quantity.Value()
		memoryKiloBytes = float64(memoryBytes) / util.KilobytesToBytes
		assert.Equal(t,  resource.Used, memoryKiloBytes)
	}
}

func TestKubeQuotaWithMissingAllocations(t *testing.T) {
	namespace := "ns1"
	cluster := "cluster1"

	clusterResources := map[metrics.ResourceType]*KubeDiscoveredResource{
		metrics.CPU: &KubeDiscoveredResource{Type:metrics.CPU, Capacity: 8.0,},
		metrics.Memory: &KubeDiscoveredResource{Type:metrics.Memory, Capacity: 16*1024,},
	}

	for _, testQuota := range TestQuotas {
		hardResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU: resource.MustParse(testQuota.cpuLimit),
		}
		usedResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU: resource.MustParse(testQuota.cpuUsed),
		}

		quota := &v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: testQuota.name,
				UID: types.UID(testQuota.name),
				Namespace: namespace,
			},
			Status: v1.ResourceQuotaStatus{
				Hard: hardResourceList,
				Used: usedResourceList,
			},
		}
		var quotaList []*v1.ResourceQuota
		quotaList = append(quotaList, quota)

		kubeQuota := CreateDefaultQuota(cluster, namespace, clusterResources)
		kubeQuota.ReconcileQuotas(quotaList)

		resource, _ := kubeQuota.GetAllocationResource(metrics.CPULimit)
		quantity := hardResourceList[v1.ResourceLimitsCPU]
		cpuMilliCore := quantity.MilliValue()
		cpuCore := float64(cpuMilliCore) / util.MilliToUnit
		assert.Equal(t, resource.Capacity, cpuCore)

		quantity = usedResourceList[v1.ResourceLimitsCPU]
		cpuMilliCore = quantity.MilliValue()
		cpuCore = float64(cpuMilliCore) / util.MilliToUnit
		assert.Equal(t, resource.Used, cpuCore)

		resource, _ = kubeQuota.GetAllocationResource(metrics.MemoryLimit)
		assert.Equal(t, resource.Capacity, clusterResources[metrics.Memory].Capacity)
		assert.Equal(t, resource.Used, 0.0)

		for _, testNode := range TestNodeProviders {
			allocationResources := map[metrics.ResourceType]float64{
				metrics.CPULimit: testNode.cpuUsed,
				metrics.MemoryLimit: testNode.memUsed,
			}
			kubeQuota.AddNodeProvider(testNode.node, allocationResources)
		}

		for _, testNode := range TestNodeProviders {
			provider := kubeQuota.GetProvider(testNode.node)
			assert.NotNil(t, provider)
			assert.Equal(t, 0, len(provider.BoughtCompute))
			resource, _ := kubeQuota.GetBoughtResource(testNode.node, metrics.CPULimit)
			assert.Equal(t, resource.Used, testNode.cpuUsed)
			resource, _ = kubeQuota.GetBoughtResource(testNode.node, metrics.MemoryLimit)
			assert.Equal(t, resource.Used, testNode.memUsed)
		}
		fmt.Printf("%s\n", kubeQuota.KubeEntity)
	}
}

func TestKubeQuotaReconcile(t *testing.T) {
	namespace := "ns1"
	cluster := "cluster1"

	var clusterResources map[metrics.ResourceType]*KubeDiscoveredResource
	var quotaList []*v1.ResourceQuota
	var leastCpuCore, leastMemKB float64
	for _, testQuota := range TestQuotas {

		quantity := resource.MustParse(testQuota.cpuLimit)
		cpuMilliCore := quantity.MilliValue()
		cpuCore := float64(cpuMilliCore) / util.MilliToUnit

		quantity = resource.MustParse(testQuota.memLimit)
		memoryBytes := quantity.Value()
		memoryKiloBytes := float64(memoryBytes) / util.KilobytesToBytes

		if leastCpuCore == 0.0 || cpuCore < leastCpuCore {
			leastCpuCore = cpuCore
		}

		if leastMemKB == 0.0 || memoryKiloBytes < leastMemKB {
			leastMemKB = memoryKiloBytes
		}

		hardResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU: resource.MustParse(testQuota.cpuLimit),
			v1.ResourceLimitsMemory: resource.MustParse(testQuota.memLimit),
		}
		usedResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU: resource.MustParse(testQuota.cpuUsed),
			v1.ResourceLimitsMemory: resource.MustParse(testQuota.memUsed),
		}

		quota := &v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: testQuota.name,
				UID: types.UID(testQuota.name),
				Namespace: namespace,
			},
			Status: v1.ResourceQuotaStatus{
				Hard: hardResourceList,
				Used: usedResourceList,
			},
		}
		quotaList = append(quotaList, quota)
	}
	kubeQuota := CreateDefaultQuota(cluster, namespace, clusterResources)
	kubeQuota.ReconcileQuotas(quotaList)
	fmt.Printf("%s\n", kubeQuota.KubeEntity)

	resource, _ := kubeQuota.GetAllocationResource(metrics.CPULimit)
	assert.Equal(t,  resource.Capacity, leastCpuCore)	// the least of the 3 quotas

	resource, _ = kubeQuota.GetAllocationResource(metrics.MemoryLimit)
	assert.Equal(t, resource.Capacity, leastMemKB)	// the least of the 3 quotas
}
