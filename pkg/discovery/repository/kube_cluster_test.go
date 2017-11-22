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
)

var TestNodes = []struct {
	name string
	cpuCap float64
	memCap float64
	cluster string
}{
	{"node1", 4.0, 800, "cluster1"},
	{"node2", 5.0, 700, "cluster1"},
	{"node3", 6.0, 600, "cluster1"},
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
		assert.Equal(t,  resource.Capacity, testNode.cpuCap*1000)
		resource, _ = kubenode.GetComputeResource(metrics.Memory)
		assert.Equal(t, resource.Capacity, testNode.memCap*1000)

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
	cpuCap float64
	cpuUsed float64
	memCap float64
	memUsed float64
}{
	{"quota1", 4.0, 3.0, 800, 500},
	{"quota2", 5.0, 3.0, 700, 500},
	{"quota3", 6.0, 3.0, 600, 500},
}

var TestNodeProviders = []struct {
	node string
	cpuUsed float64
	memUsed float64
} {
	{"node1", 1.0, 100},
	{"node2", 0.5, 150},
	{"node3", 1.5, 250},
}

func TestKubeQuota(t *testing.T) {
	namespace := "ns1"
	cluster := "cluster1"
	var quotaList []*v1.ResourceQuota
	for _, testQuota := range TestQuotas {
		hardResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU: resource.MustParse(fmt.Sprint(testQuota.cpuCap)),
			v1.ResourceLimitsMemory: resource.MustParse(fmt.Sprint(testQuota.memCap)),
		}
		usedResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU: resource.MustParse(fmt.Sprint(testQuota.cpuUsed)),
			v1.ResourceLimitsMemory: resource.MustParse(fmt.Sprint(testQuota.memUsed)),
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

	kubeQuota := CreateKubeQuota(cluster, namespace, quotaList)
	assert.Equal(t, kubeQuota.Name, namespace)
	assert.Equal(t, kubeQuota.UID, namespace)
	resource, _ := kubeQuota.GetAllocationResource(metrics.CPULimit)
	assert.Equal(t,  resource.Capacity, 4.0*1000)	// the least of the 3 quotas
	assert.Equal(t,  resource.Used, 3.0*1000)
	resource, _ = kubeQuota.GetAllocationResource(metrics.MemoryLimit)
	assert.Equal(t, resource.Capacity, 600.0*1000)	// the least of the 3 quotas
	assert.Equal(t,  resource.Used, 500.0*1000)

	for _, testNode := range TestNodeProviders {
		allocationResources := map[metrics.ResourceType]float64 {
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
		assert.Equal(t,  resource.Used, testNode.cpuUsed)
		resource, _ = kubeQuota.GetBoughtResource(testNode.node, metrics.MemoryLimit)
		assert.Equal(t,  resource.Used, testNode.memUsed)
	}

	fmt.Printf("%s\n", kubeQuota.KubeEntity)
}
