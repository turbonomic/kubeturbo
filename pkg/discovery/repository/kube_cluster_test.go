package repository

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var TestNodes = []struct {
	name    string
	cpuCap  float64
	memCap  float64
	cluster string
}{
	{"node1", 4000.0, 819200, "cluster1"},
	{"node2", 5000.0, 614400, "cluster1"},
	{"node3", 6000.0, 409600, "cluster1"},
}

func TestKubeNode(t *testing.T) {
	for _, testNode := range TestNodes {
		resourceList := v1.ResourceList{
			// We query cpu capacity as milicores from node properties.
			// What we set here is cpu cores.
			v1.ResourceCPU:    resource.MustParse(fmt.Sprintf("%d", int(testNode.cpuCap/1000))),
			v1.ResourceMemory: resource.MustParse(fmt.Sprint(testNode.memCap)),
		}

		n1 := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNode.name,
				UID:  types.UID(testNode.name),
			},
			Status: v1.NodeStatus{
				Allocatable: resourceList,
			},
		}

		kubenode := NewKubeNode(n1, testNode.cluster)

		resource, _ := kubenode.GetComputeResource(metrics.CPUMili)
		assert.Equal(t, resource.Capacity, testNode.cpuCap)
		resource, _ = kubenode.GetComputeResource(metrics.Memory)
		assert.Equal(t, resource.Capacity, testNode.memCap/1024)

		resource, _ = kubenode.GetComputeResource(metrics.CPULimitQuota)
		assert.Nil(t, resource)
		resource, _ = kubenode.GetComputeResource(metrics.MemoryLimitQuota)
		assert.Nil(t, resource)

		resource, _ = kubenode.GetAllocationResource(metrics.CPUMili)
		assert.Nil(t, resource)
		resource, _ = kubenode.GetAllocationResource(metrics.Memory)
		assert.Nil(t, resource)
	}
}

var TestQuotas = []struct {
	name     string
	cpuLimit string
	cpuUsed  string
	memLimit string
	memUsed  string
}{
	{"quota1", "4", "3", "8Gi", "5Gi"},
	{"quota2", "0.5", "0.3", "7Mi", "5Gi"},
	{"quota3", "6000m", "300m", "6Ki", "5Ki"},
}

func TestKubeNamespace(t *testing.T) {
	namespace := "ns1"
	cluster := "cluster1"

	for i, testQuota := range TestQuotas {
		uuid := fmt.Sprintf("namespace-%d", i)
		kubeNamespace := CreateDefaultKubeNamespace(cluster, namespace, uuid)
		hardResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU:    resource.MustParse(testQuota.cpuLimit),
			v1.ResourceLimitsMemory: resource.MustParse(testQuota.memLimit),
		}
		usedResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU:    resource.MustParse(testQuota.cpuUsed),
			v1.ResourceLimitsMemory: resource.MustParse(testQuota.memUsed),
		}

		quota := &v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testQuota.name,
				UID:       types.UID(testQuota.name),
				Namespace: namespace,
			},
			Status: v1.ResourceQuotaStatus{
				Hard: hardResourceList,
				Used: usedResourceList,
			},
		}
		var quotaList []*v1.ResourceQuota
		quotaList = append(quotaList, quota)

		kubeNamespace.ReconcileQuotas(quotaList)

		resource, _ := kubeNamespace.GetAllocationResource(metrics.CPULimitQuota)
		quantity := hardResourceList[v1.ResourceLimitsCPU]
		cpuMilliCore := quantity.MilliValue()
		cpuCore := util.MetricMilliToUnit(float64(cpuMilliCore))
		assert.Equal(t, resource.Capacity, cpuCore)

		quantity = usedResourceList[v1.ResourceLimitsCPU]
		cpuMilliCore = quantity.MilliValue()
		cpuCore = util.MetricMilliToUnit(float64(cpuMilliCore))
		assert.Equal(t, resource.Used, cpuCore)

		resource, _ = kubeNamespace.GetAllocationResource(metrics.MemoryLimitQuota)
		quantity = hardResourceList[v1.ResourceLimitsMemory]
		memoryBytes := quantity.Value()
		memoryKiloBytes := util.Base2BytesToKilobytes(float64(memoryBytes))
		assert.Equal(t, resource.Capacity, memoryKiloBytes) // the least of the 3 quotas
		quantity = usedResourceList[v1.ResourceLimitsMemory]
		memoryBytes = quantity.Value()
		memoryKiloBytes = util.Base2BytesToKilobytes(float64(memoryBytes))
		assert.Equal(t, resource.Used, memoryKiloBytes)
	}
}

func TestKubeNamespaceWithMissingAllocations(t *testing.T) {
	namespace := "ns1"
	cluster := "cluster1"

	for i, testQuota := range TestQuotas {
		hardResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU: resource.MustParse(testQuota.cpuLimit),
		}
		usedResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU: resource.MustParse(testQuota.cpuUsed),
		}

		quota := &v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testQuota.name,
				UID:       types.UID(testQuota.name),
				Namespace: namespace,
			},
			Status: v1.ResourceQuotaStatus{
				Hard: hardResourceList,
				Used: usedResourceList,
			},
		}
		var quotaList []*v1.ResourceQuota
		quotaList = append(quotaList, quota)

		uuid := fmt.Sprintf("namespace-%d", i)
		kubeNamespace := CreateDefaultKubeNamespace(cluster, namespace, uuid)
		kubeNamespace.ReconcileQuotas(quotaList)

		resource, _ := kubeNamespace.GetAllocationResource(metrics.CPULimitQuota)
		quantity := hardResourceList[v1.ResourceLimitsCPU]
		cpuMilliCore := quantity.MilliValue()
		cpuCore := util.MetricMilliToUnit(float64(cpuMilliCore))
		assert.Equal(t, resource.Capacity, cpuCore)

		quantity = usedResourceList[v1.ResourceLimitsCPU]
		cpuMilliCore = quantity.MilliValue()
		cpuCore = util.MetricMilliToUnit(float64(cpuMilliCore))
		assert.Equal(t, resource.Used, cpuCore)

		resource, _ = kubeNamespace.GetAllocationResource(metrics.MemoryLimitQuota)
		assert.Equal(t, resource.Capacity, DEFAULT_METRIC_CAPACITY_VALUE)
		assert.Equal(t, resource.Used, 0.0)
	}
}

func TestKubeNamespaceQuotaReconcile(t *testing.T) {
	namespace := "ns1"
	cluster := "cluster1"

	var quotaList []*v1.ResourceQuota
	var leastCpuCore, leastMemKB float64
	for _, testQuota := range TestQuotas {

		quantity := resource.MustParse(testQuota.cpuLimit)
		cpuMilliCore := quantity.MilliValue()
		cpuCore := util.MetricMilliToUnit(float64(cpuMilliCore))

		quantity = resource.MustParse(testQuota.memLimit)
		memoryBytes := quantity.Value()
		memoryKiloBytes := util.Base2BytesToKilobytes(float64(memoryBytes))

		if leastCpuCore == 0.0 || cpuCore < leastCpuCore {
			leastCpuCore = cpuCore
		}

		if leastMemKB == 0.0 || memoryKiloBytes < leastMemKB {
			leastMemKB = memoryKiloBytes
		}

		hardResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU:    resource.MustParse(testQuota.cpuLimit),
			v1.ResourceLimitsMemory: resource.MustParse(testQuota.memLimit),
		}
		usedResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU:    resource.MustParse(testQuota.cpuUsed),
			v1.ResourceLimitsMemory: resource.MustParse(testQuota.memUsed),
		}

		quota := &v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testQuota.name,
				UID:       types.UID(testQuota.name),
				Namespace: namespace,
			},
			Status: v1.ResourceQuotaStatus{
				Hard: hardResourceList,
				Used: usedResourceList,
			},
		}
		quotaList = append(quotaList, quota)
	}
	kubeNamespace := CreateDefaultKubeNamespace(cluster, namespace, "namespace-uuid")
	kubeNamespace.ReconcileQuotas(quotaList)

	resource, _ := kubeNamespace.GetAllocationResource(metrics.CPULimitQuota)
	assert.Equal(t, resource.Capacity, leastCpuCore) // the least of the 3 quotas

	resource, _ = kubeNamespace.GetAllocationResource(metrics.MemoryLimitQuota)
	assert.Equal(t, resource.Capacity, leastMemKB) // the least of the 3 quotas
}

func TestNamespaceNames(t *testing.T) {
	clusterName := "k8s-cluster"
	namespaceName := "kube-system"
	namespaceId := "21c65de7-f4e9-11e7-acc0-005056802f41"

	kubeNamespace := CreateDefaultKubeNamespace(clusterName, namespaceName, namespaceId)

	if kubeNamespace.UID != namespaceId {
		t.Errorf("kubeNamespace.UID is wrong: %v Vs. %v", kubeNamespace.UID, namespaceId)
	}

	if kubeNamespace.ClusterName != clusterName {
		t.Errorf("kubeNamespace.clusterName is wrong:%v Vs. %v", kubeNamespace.ClusterName, clusterName)
	}

	if kubeNamespace.Name != namespaceName {
		t.Errorf("kubeNamespace.name is wrong: %v Vs. %v", kubeNamespace.Name, namespaceName)
	}
}
