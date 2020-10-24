package worker

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var (
	cluster1 = "cluster1"
	node1    = "node1"
	node2    = "node2"
	ns1      = "ns1" // one pod each on n1 and n2
	ns2      = "ns2" // one pod each on n1 and n2
	ns3      = "ns3" // all pods on one node
	ns4      = "ns4" // no pods

	nodeCpuCap = 4.0
	nodeMemCap = 8010840.0

	n1 = &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node1,
			UID:  types.UID(node1),
			// Resources TODO:
		},
		Status: v1.NodeStatus{
			Allocatable: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("4.0"),
			},
		},
	}

	n2 = &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node2,
			UID:  types.UID(node2),
			// Resources TODO:
		},
		Status: v1.NodeStatus{
			Allocatable: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU: resource.MustParse("4.0"),
			},
		},
	}

	container1 = mockContainer("container1", cpuRequests_container1, cpuLimits_container1, 0, 0)
	container2 = mockContainer("container2", cpuRequests_container2, cpuLimits_container2, 0, 0)

	// Pod on node n1 and namespace ns1
	pod_ns1_n1 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod11",
			Namespace: ns1,
		},
		Spec: v1.PodSpec{
			NodeName: node1,
			Containers: []v1.Container{
				container1,
			},
		},
	}

	// Pod on node n2 and namespace ns1
	pod_ns1_n2 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod12",
			Namespace: ns1,
		},
		Spec: v1.PodSpec{
			NodeName: node2,
			Containers: []v1.Container{
				container2,
			},
		},
	}

	// Pod on node n1 and namespace ns2
	pod_ns2_n1 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod21",
			Namespace: ns2,
		},
		Spec: v1.PodSpec{
			NodeName: node1,
		},
	}

	// Pod on node n2 and namespace ns2
	pod_ns2_n2 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod22",
			Namespace: ns2,
		},
		Spec: v1.PodSpec{
			NodeName: node2,
		},
	}

	// Pod on node n1 and namespace ns3
	pod1_ns3_n1 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod31",
			Namespace: ns3,
		},
		Spec: v1.PodSpec{
			NodeName: node1,
		},
	}

	// Pod on node n1 and namespace ns3
	pod2_ns3_n1 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod32",
			Namespace: ns3,
		},
		Spec: v1.PodSpec{
			NodeName: node1,
		},
	}

	kubeNode1 = repository.NewKubeNode(n1, cluster1)

	kubeNode2 = repository.NewKubeNode(n2, cluster1)

	kubens1 = repository.CreateDefaultKubeNamespace(cluster1, ns1, "namespace-uuid1")
	kubens2 = repository.CreateDefaultKubeNamespace(cluster1, ns2, "namespace-uuid2")
	kubens3 = repository.CreateDefaultKubeNamespace(cluster1, ns3, "namespace-uuid3")
	kubens4 = repository.CreateDefaultKubeNamespace(cluster1, ns4, "namespace-uuid4")

	kubeCluster = &repository.KubeCluster{
		Name: cluster1,
		Nodes: map[string]*repository.KubeNode{
			node1: kubeNode1,
			node2: kubeNode2,
		},
		Namespaces: map[string]*repository.KubeNamespace{
			ns1: kubens1,
			ns2: kubens2,
			ns3: kubens3,
			ns4: kubens4,
		},
	}

	cpuUsed_pod_n1_ns1        = 2.5
	cpuUsed_pod_n2_ns1        = 2.5
	cpuRequestUsed_pod_n1_ns1 = 1.5
	cpuRequestUsed_pod_n2_ns1 = 1.5

	cpuLimits_container1   = 2.0
	cpuRequests_container1 = 1.0
	cpuLimits_container2   = 2.0
	cpuRequests_container2 = 1.0

	cpuUsed_pod_n1_ns2        = 2.0
	cpuUsed_pod_n2_ns2        = 2.0
	cpuRequestUsed_pod_n1_ns2 = 1.0
	cpuRequestUsed_pod_n2_ns2 = 1.0

	cpuUsed_pod1_n1_ns3        = 1.5
	cpuUsed_pod2_n1_ns3        = 1.0
	cpuRequestUsed_pod1_n1_ns3 = 1.0
	cpuRequestUsed_pod2_n1_ns3 = 0.5

	// Pod metrics
	metricsSink = metrics.NewEntityMetricSink()
	etype       = metrics.PodType
	// Pod in ns1 on n1
	nowUtcSec = time.Now().Unix()
	metric_cpuUsed_pod_n1_ns1        = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns1_n1), metrics.CPU, metrics.Used, []metrics.Point{{cpuUsed_pod_n1_ns1, nowUtcSec}})
	metric_cpuCap_pod_n1_ns1         = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns1_n1), metrics.CPU, metrics.Capacity, nodeCpuCap)
	metric_cpuRequestUsed_pod_n1_ns1 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns1_n1), metrics.CPURequest, metrics.Used, cpuRequestUsed_pod_n1_ns1)
	metric_cpuRequestCap_pod_n1_ns1  = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns1_n1), metrics.CPURequest, metrics.Capacity, nodeCpuCap)
	// Pod in ns1 on n2
	metric_cpuUsed_pod_n2_ns1        = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns1_n2), metrics.CPU, metrics.Used, []metrics.Point{{cpuUsed_pod_n2_ns1, nowUtcSec}})
	metric_cpuRequestUsed_pod_n2_ns1 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns1_n2), metrics.CPURequest, metrics.Used, cpuRequestUsed_pod_n2_ns1)
	// Pod in ns2 on n1
	metric_cpuUsed_pod_n1_ns2        = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns2_n1), metrics.CPU, metrics.Used, []metrics.Point{{cpuUsed_pod_n1_ns2, nowUtcSec}})
	metric_cpuCap_pod_n1_ns2         = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns2_n1), metrics.CPU, metrics.Capacity, nodeCpuCap)
	metric_cpuRequestUsed_pod_n1_ns2 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns2_n1), metrics.CPURequest, metrics.Used, cpuRequestUsed_pod_n1_ns2)
	metric_cpuRequestCap_pod_n1_ns2  = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns2_n1), metrics.CPURequest, metrics.Capacity, nodeCpuCap)

	// Pod1 and Pod2 in ns3 on n1
	metric_cpuUsed_pod1_n1_ns3        = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod1_ns3_n1), metrics.CPU, metrics.Used, []metrics.Point{{cpuUsed_pod1_n1_ns3, nowUtcSec}})
	metric_cpuCap_pod1_n1_ns3         = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod1_ns3_n1), metrics.CPU, metrics.Capacity, nodeCpuCap)
	metric_cpuUsed_pod2_n1_ns3        = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod2_ns3_n1), metrics.CPU, metrics.Used, []metrics.Point{{cpuUsed_pod2_n1_ns3, nowUtcSec}})
	metric_cpuCap_pod2_n1_ns3         = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod2_ns3_n1), metrics.CPU, metrics.Capacity, nodeCpuCap)
	metric_cpuRequestUsed_pod1_n1_ns3 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod1_ns3_n1), metrics.CPURequest, metrics.Used, cpuRequestUsed_pod1_n1_ns3)
	metric_cpuRequestCap_pod1_n1_ns3  = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod1_ns3_n1), metrics.CPURequest, metrics.Capacity, nodeCpuCap)
	metric_cpuRequestUsed_pod2_n1_ns3 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod2_ns3_n1), metrics.CPURequest, metrics.Used, cpuRequestUsed_pod2_n1_ns3)
	metric_cpuRequestCap_pod2_n1_ns3  = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod2_ns3_n1), metrics.CPURequest, metrics.Capacity, nodeCpuCap)

	// CPU used for node n1 from pods
	cpuUsed_pod_n1 = cpuUsed_pod_n1_ns1 + cpuUsed_pod_n1_ns2 + cpuUsed_pod1_n1_ns3 + cpuUsed_pod2_n1_ns3
	cpuUsed_pod_n2 = cpuUsed_pod_n2_ns1 + cpuUsed_pod_n2_ns2
	// CPURequest used for node n1 from pods
	cpuRequestUsed_pod_n1 = cpuRequestUsed_pod_n1_ns1 + cpuRequestUsed_pod_n1_ns2 + cpuRequestUsed_pod1_n1_ns3 + cpuRequestUsed_pod2_n1_ns3
	cpuRequestUsed_pod_n2 = cpuRequestUsed_pod_n2_ns1 + cpuRequestUsed_pod_n2_ns2

	nodeToPodsMap = map[string][]*v1.Pod{
		node1: {pod_ns1_n1, pod_ns2_n1, pod1_ns3_n1, pod2_ns3_n1},
		node2: {pod_ns1_n2, pod_ns2_n2},
	}

	quotaToPodsMap = map[string]map[string][]*v1.Pod{
		ns1: {node1: {pod_ns1_n1},
			node2: {pod_ns1_n2}},
		ns2: {node1: {pod_ns2_n1},
			node2: {pod_ns2_n2}},
		ns3: {node1: {pod1_ns3_n1, pod2_ns3_n1},
			node2: {}},
		ns4: {node1: {},
			node2: {}},
	}
)

func TestPodMetricsListAllocationUsage(t *testing.T) {
	metricsSink.AddNewMetricEntries(
		metric_cpuUsed_pod_n1_ns1,
		metric_cpuUsed_pod_n2_ns1,
		metric_cpuRequestUsed_pod_n1_ns1,
		metric_cpuRequestUsed_pod_n2_ns1,
	)
	var podMetricsList PodMetricsList
	pm1 := createPodMetrics(pod_ns1_n1, ns1, metricsSink)
	pm2 := createPodMetrics(pod_ns1_n2, ns1, metricsSink)
	podMetricsList = append(podMetricsList, pm1)
	podMetricsList = append(podMetricsList, pm2)

	resourceMap := podMetricsList.SumQuotaUsage()
	cpuLimitQuotaUsed := cpuLimits_container1 + cpuLimits_container2
	cpuRequestQuotaUsed := cpuRequests_container1 + cpuRequests_container2
	memLimitQuotaUsed := 0.0
	memRequestQuotaUsed := 0.0
	assert.Equal(t, cpuLimitQuotaUsed, resourceMap[metrics.CPULimitQuota])
	assert.Equal(t, memLimitQuotaUsed, resourceMap[metrics.MemoryLimitQuota])
	assert.Equal(t, cpuRequestQuotaUsed, resourceMap[metrics.CPURequestQuota])
	assert.Equal(t, memRequestQuotaUsed, resourceMap[metrics.MemoryRequestQuota])
}

func TestPodMetricsCollectionNullCluster(t *testing.T) {
	collector := &MetricsCollector{
		MetricsSink: metricsSink,
		NodeList:    []*v1.Node{n1},
		PodList:     nodeToPodsMap[node1],
	}

	podCollection, err := collector.CollectPodMetrics()
	assert.NotNil(t, err)
	assert.Nil(t, podCollection)
}

func TestPodMetricsCollectionUnknownNamespace(t *testing.T) {
	namespace := "Namespace_Unknown"
	clusterSummary := repository.CreateClusterSummary(kubeCluster)
	pod1 := &v1.Pod{}
	pod1.ObjectMeta.Name = "pod1"
	pod1.ObjectMeta.Namespace = namespace
	pod1.Spec.NodeName = node1

	pod2 := &v1.Pod{}
	pod2.ObjectMeta.Name = "pod2"
	pod2.ObjectMeta.Namespace = namespace
	pod2.Spec.NodeName = node2

	collector := &MetricsCollector{
		Cluster:     clusterSummary,
		MetricsSink: metricsSink,
		PodList:     []*v1.Pod{pod1, pod2},
		NodeList:    []*v1.Node{n1},
	}

	podCollection, _ := collector.CollectPodMetrics()
	assert.Equal(t, 0, len(podCollection))
}

func TestPodMetricsCollectionSingleNode(t *testing.T) {
	// Sink contains cpu and cpuRequest capacity and used for all pods on node1
	metricsSink.AddNewMetricEntries(
		metric_cpuUsed_pod_n1_ns1,
		metric_cpuUsed_pod_n1_ns2,
		metric_cpuUsed_pod1_n1_ns3,
		metric_cpuUsed_pod2_n1_ns3,
		metric_cpuRequestUsed_pod_n1_ns1,
		metric_cpuRequestUsed_pod_n1_ns2,
		metric_cpuRequestUsed_pod1_n1_ns3,
		metric_cpuRequestUsed_pod2_n1_ns3,
	)

	metricsSink.AddNewMetricEntries(
		metric_cpuCap_pod_n1_ns1,
		metric_cpuCap_pod_n1_ns2,
		metric_cpuCap_pod1_n1_ns3,
		metric_cpuCap_pod2_n1_ns3,
		metric_cpuRequestCap_pod_n1_ns1,
		metric_cpuRequestCap_pod_n1_ns2,
		metric_cpuRequestCap_pod1_n1_ns3,
		metric_cpuRequestCap_pod2_n1_ns3,
	)

	// Set limits for ns1 and ns2 for CPU,
	// compute capacity for the pods in these namespaces will be changed to the quota limit value
	_ = kubens1.SetResourceCapacity(metrics.CPULimitQuota, 3.0)
	_ = kubens2.SetResourceCapacity(metrics.CPULimitQuota, 2.0)
	_ = kubens3.SetResourceCapacity(metrics.CPURequestQuota, 3.0)
	_ = kubens4.SetResourceCapacity(metrics.CPURequestQuota, 2.0)

	clusterSummary := repository.CreateClusterSummary(kubeCluster)
	collector := &MetricsCollector{
		Cluster:     clusterSummary,
		MetricsSink: metricsSink,
		PodList:     nodeToPodsMap[node1],
		NodeList:    []*v1.Node{n1},
	}

	podCollection, err := collector.CollectPodMetrics()
	assert.Nil(t, err)
	assert.NotNil(t, podCollection)
	_, exists := podCollection[node1]
	assert.True(t, exists)

	_, exists = podCollection[node2]
	assert.False(t, exists)

	for node, podsByQuotaMap := range podCollection {
		for quota, podMetricsList := range podsByQuotaMap {
			pmMap := make(map[string]*repository.PodMetrics)
			// pods in ns1
			for _, pm := range podMetricsList {
				pmMap[pm.PodName] = pm
			}

			quotaPods := quotaToPodsMap[quota][node]
			for _, pod := range quotaPods {
				pm, exists := pmMap[pod.Name]
				assert.True(t, exists)
				assert.NotNil(t, pm)
			}

			for _, podMetrics := range podMetricsList {
				// assert that the metrics is created for all allocation resources
				allocationMap := podMetrics.QuotaUsed
				for _, allocationType := range metrics.QuotaResources {
					_, exists := allocationMap[allocationType]
					assert.True(t, exists)
				}

				// pod quota capacity map from quota resource type to capacity value
				quotaCapacity := podMetrics.QuotaCapacity
				kubeNamespace := collector.Cluster.NamespaceMap[podMetrics.Namespace]

				expectedCPULimitQuota, _ := kubeNamespace.GetAllocationResource(metrics.CPULimitQuota)
				podCPULimitQuotaCap, exists := quotaCapacity[metrics.CPULimitQuota]
				assert.True(t, exists)
				assert.Equal(t, expectedCPULimitQuota.Capacity, podCPULimitQuotaCap)

				expectedCPURequestQuota, _ := kubeNamespace.GetAllocationResource(metrics.CPURequestQuota)
				podCPURequestQuotaCap, exists := quotaCapacity[metrics.CPURequestQuota]
				assert.True(t, exists)
				assert.Equal(t, expectedCPURequestQuota.Capacity, podCPURequestQuotaCap)
			}
		}
	}
}

func mockContainer(name string, requestCores, limitCores float64,
	requestMB, limitMB int64) v1.Container {
	container := v1.Container{
		Name: name,
		Resources: v1.ResourceRequirements{
			Limits:   buildResource(limitCores, limitMB),
			Requests: buildResource(requestCores, requestMB),
		},
	}
	return container
}

func buildResource(cores float64, numMB int64) v1.ResourceList {
	resourceList := make(v1.ResourceList)
	resourceList[v1.ResourceCPU] = genCPUQuantity(cores)
	resourceList[v1.ResourceMemory] = genMemQuantity(numMB)
	return resourceList
}

func genCPUQuantity(cores float64) resource.Quantity {
	result, _ := resource.ParseQuantity(fmt.Sprintf("%dm", int(cores*1000)))
	return result
}

func genMemQuantity(numKB int64) resource.Quantity {
	result, _ := resource.ParseQuantity(fmt.Sprintf("%dMi", numKB))
	return result
}
