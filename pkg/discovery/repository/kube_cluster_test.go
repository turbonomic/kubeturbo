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
			// We query cpu capacity as millicores from node properties.
			// What we set here to be parsed is cpu cores.
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

		resource, _ := kubenode.GetComputeResource(metrics.CPU)
		assert.Equal(t, resource.Capacity, testNode.cpuCap)
		resource, _ = kubenode.GetComputeResource(metrics.Memory)
		assert.Equal(t, resource.Capacity, testNode.memCap/1024)

		resource, _ = kubenode.GetComputeResource(metrics.CPULimitQuota)
		assert.Nil(t, resource)
		resource, _ = kubenode.GetComputeResource(metrics.MemoryLimitQuota)
		assert.Nil(t, resource)

		resource, _ = kubenode.GetAllocationResource(metrics.CPU)
		assert.Nil(t, resource)
		resource, _ = kubenode.GetAllocationResource(metrics.Memory)
		assert.Nil(t, resource)
	}
}

var TestQuotas = []struct {
	name     string
	cpuLimit string
	memLimit string
}{
	{"quota1", "4", "8Gi"},
	{"quota2", "0.5", "7Mi"},
	{"quota3", "6000m", "6Ki"},
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

		quota := &v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testQuota.name,
				UID:       types.UID(testQuota.name),
				Namespace: namespace,
			},
			Status: v1.ResourceQuotaStatus{
				Hard: hardResourceList,
			},
		}
		var quotaList []*v1.ResourceQuota
		quotaList = append(quotaList, quota)

		kubeNamespace.ReconcileQuotas(quotaList)

		resource, _ := kubeNamespace.GetAllocationResource(metrics.CPULimitQuota)
		quantity := hardResourceList[v1.ResourceLimitsCPU]
		cpuMilliCore := quantity.MilliValue()
		assert.Equal(t, resource.Capacity, float64(cpuMilliCore))

		resource, _ = kubeNamespace.GetAllocationResource(metrics.MemoryLimitQuota)
		quantity = hardResourceList[v1.ResourceLimitsMemory]
		memoryBytes := quantity.Value()
		memoryKiloBytes := util.Base2BytesToKilobytes(float64(memoryBytes))
		assert.Equal(t, resource.Capacity, memoryKiloBytes) // the least of the 3 quotas
	}
}

func TestKubeNamespaceWithMissingAllocations(t *testing.T) {
	namespace := "ns1"
	cluster := "cluster1"

	for i, testQuota := range TestQuotas {
		hardResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU: resource.MustParse(testQuota.cpuLimit),
		}

		quota := &v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testQuota.name,
				UID:       types.UID(testQuota.name),
				Namespace: namespace,
			},
			Status: v1.ResourceQuotaStatus{
				Hard: hardResourceList,
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
		assert.Equal(t, resource.Capacity, float64(cpuMilliCore))

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
		cpuMilliCore := float64(quantity.MilliValue())

		quantity = resource.MustParse(testQuota.memLimit)
		memoryBytes := quantity.Value()
		memoryKiloBytes := util.Base2BytesToKilobytes(float64(memoryBytes))

		if leastCpuCore == 0.0 || cpuMilliCore < leastCpuCore {
			leastCpuCore = cpuMilliCore
		}

		if leastMemKB == 0.0 || memoryKiloBytes < leastMemKB {
			leastMemKB = memoryKiloBytes
		}

		hardResourceList := v1.ResourceList{
			v1.ResourceLimitsCPU:    resource.MustParse(testQuota.cpuLimit),
			v1.ResourceLimitsMemory: resource.MustParse(testQuota.memLimit),
		}

		quota := &v1.ResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testQuota.name,
				UID:       types.UID(testQuota.name),
				Namespace: namespace,
			},
			Status: v1.ResourceQuotaStatus{
				Hard: hardResourceList,
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

type ClusterResultSummary struct {
	S      ClusterSummary
	Result map[string]bool
}

func TestComputeStaticPodToDaemonMap(t *testing.T) {

	clusterSummaries := []struct {
		s      *ClusterSummary
		result map[string]bool
	}{
		// empty nodes and empty pods test
		{
			s: &ClusterSummary{
				KubeCluster: &KubeCluster{
					Nodes: make([]*v1.Node, 0),
					Pods:  make([]*v1.Pod, 0),
				},
			},
			result: make(map[string]bool),
		},
		// no node pools single node cluster test
		{
			s: &ClusterSummary{
				KubeCluster: &KubeCluster{
					Nodes: []*v1.Node{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-node",
							},
						},
					},
					Pods: []*v1.Pod{
						{
							Spec: v1.PodSpec{
								NodeName: "test-node",
							},
							ObjectMeta: metav1.ObjectMeta{
								UID:  "pod1-UID",
								Name: "test-pod-prefix-test-node",
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind: util.Kind_Node,
										Name: "test-node",
									},
								},
							},
						},
					},
				},
			},
			result: map[string]bool{
				"pod1-UID": true,
			},
		},
		// multiple nodes and multiple pod cluster
		{
			s: &ClusterSummary{
				KubeCluster: &KubeCluster{
					Nodes: []*v1.Node{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-node",
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-node2",
							},
						},
					},
					Pods: []*v1.Pod{
						// static pod apart of daemon set
						{
							Spec: v1.PodSpec{
								NodeName: "test-node",
							},
							ObjectMeta: metav1.ObjectMeta{
								UID:  "pod1-UID",
								Name: "test-pod-prefix-test-node",
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind: util.Kind_Node,
										Name: "test-node",
									},
								},
							},
						},
						// static pod not apart of daemon set
						{
							Spec: v1.PodSpec{
								NodeName: "test-node",
							},
							ObjectMeta: metav1.ObjectMeta{
								UID:  "pod2-UID",
								Name: "test-pod-prefix2-test-node",
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind: util.Kind_Node,
										Name: "test-node",
									},
								},
							},
						},
						// not a static pod
						{
							Spec: v1.PodSpec{
								NodeName: "test-node",
							},
							ObjectMeta: metav1.ObjectMeta{
								UID:  "pod4-UID",
								Name: "pod4",
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind: "controller",
										Name: "test-node",
									},
								},
							},
						},
						// static pod apart of daemon set
						{
							Spec: v1.PodSpec{
								NodeName: "test-node2",
							},
							ObjectMeta: metav1.ObjectMeta{
								UID:  "pod3-UID",
								Name: "test-pod-prefix-test-node2",
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind: util.Kind_Node,
										Name: "test-node2",
									},
								},
							},
						},
					},
				},
			},
			result: map[string]bool{
				"pod1-UID": true,
				"pod2-UID": false,
				"pod3-UID": true,
			},
		},
		// cluster with node pools, multiple nodes and multiple pods
		{
			s: &ClusterSummary{
				KubeCluster: &KubeCluster{
					Nodes: []*v1.Node{
						{

							ObjectMeta: metav1.ObjectMeta{
								Name: "test-node",
								Labels: map[string]string{
									util.NodePoolGKE: util.NodePoolGKE,
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-node2",
								Labels: map[string]string{
									util.NodePoolEKSIdentifier: util.NodePoolEKSIdentifier,
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-node3",
								Labels: map[string]string{
									util.NodePoolGKE: util.NodePoolGKE,
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test-node4",
								Labels: map[string]string{
									util.NodePoolEKSIdentifier: util.NodePoolEKSIdentifier,
								},
							},
						},
					},
					Pods: []*v1.Pod{
						// static pod apart of daemon set
						{
							Spec: v1.PodSpec{
								NodeName: "test-node",
							},
							ObjectMeta: metav1.ObjectMeta{
								UID:  "pod1-UID",
								Name: "test-pod-prefix-test-node",
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind: util.Kind_Node,
										Name: "test-node",
									},
								},
							},
						},
						// static pod apart of different daemon set
						{
							Spec: v1.PodSpec{
								NodeName: "test-node2",
							},
							ObjectMeta: metav1.ObjectMeta{
								UID:  "pod2-UID",
								Name: "test-pod-prefix2-test-node2",
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind: util.Kind_Node,
										Name: "test-node2",
									},
								},
							},
						},
						// not a static pod
						{
							Spec: v1.PodSpec{
								NodeName: "test-node3",
							},
							ObjectMeta: metav1.ObjectMeta{
								UID:  "pod4-UID",
								Name: "pod4",
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind: "controller",
										Name: "test-node3",
									},
								},
							},
						},
						// static pod apart of daemon set
						{
							Spec: v1.PodSpec{
								NodeName: "test-node3",
							},
							ObjectMeta: metav1.ObjectMeta{
								UID:  "pod3-UID",
								Name: "test-pod-prefix-test-node3",
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind: util.Kind_Node,
										Name: "test-node3",
									},
								},
							},
						},
						// static pod apart of different daemon set
						{
							Spec: v1.PodSpec{
								NodeName: "test-node4",
							},
							ObjectMeta: metav1.ObjectMeta{
								UID:  "pod5-UID",
								Name: "test-pod-prefix2-test-node4",
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind: util.Kind_Node,
										Name: "test-node4",
									},
								},
							},
						},
						// static pod not apart of different daemon set
						{
							Spec: v1.PodSpec{
								NodeName: "test-node",
							},
							ObjectMeta: metav1.ObjectMeta{
								UID:  "pod6-UID",
								Name: "test-pod-prefix3-test-node",
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind: util.Kind_Node,
										Name: "test-node",
									},
								},
							},
						},
						// static pod not apart of different daemon set
						{
							Spec: v1.PodSpec{
								NodeName: "test-node2",
							},
							ObjectMeta: metav1.ObjectMeta{
								UID:  "pod7-UID",
								Name: "test-pod-prefix3-test-node2",
								OwnerReferences: []metav1.OwnerReference{
									{
										Kind: util.Kind_Node,
										Name: "test-node2",
									},
								},
							},
						},
					},
				},
			},
			result: map[string]bool{
				"pod1-UID": true,
				"pod2-UID": true,
				"pod3-UID": true,
				"pod5-UID": true,
				"pod6-UID": false,
				"pod7-UID": false,
			},
		},
	}

	for _, sum := range clusterSummaries {
		sum.s.computeStaticPodToDaemonMap()
		assert.Equal(t, sum.result, sum.s.StaticPodToDaemonMap)
	}

}
