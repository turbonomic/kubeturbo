package worker

import (
	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/pkg/api/v1"
	"testing"
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
	// Pod on node n1 and namespace ns1
	pod_ns1_n1 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod11",
			Namespace: ns1,
		},
		Spec: v1.PodSpec{
			NodeName: node1,
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

	clusterResources = map[metrics.ResourceType]*repository.KubeDiscoveredResource{
		metrics.CPU: {Type: metrics.CPU, Capacity: 8.0},
	}
	kubeQuota1 = repository.CreateDefaultQuota(cluster1, ns1, clusterResources)
	kubeQuota2 = repository.CreateDefaultQuota(cluster1, ns2, clusterResources)
	kubeQuota3 = repository.CreateDefaultQuota(cluster1, ns3, clusterResources)
	kubeQuota4 = repository.CreateDefaultQuota(cluster1, ns4, clusterResources)

	kubens1 = &repository.KubeNamespace{
		ClusterName: cluster1,
		Name:        ns1,
		Quota:       kubeQuota1,
	}

	kubens2 = &repository.KubeNamespace{
		ClusterName: cluster1,
		Name:        ns2,
		Quota:       kubeQuota2,
	}

	kubens3 = &repository.KubeNamespace{
		ClusterName: cluster1,
		Name:        ns3,
		Quota:       kubeQuota3,
	}

	kubens4 = &repository.KubeNamespace{
		ClusterName: cluster1,
		Name:        ns4,
		Quota:       kubeQuota4,
	}

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

	cpuUsed_pod_n1_ns1 = 2.5
	cpuUsed_pod_n2_ns1 = 2.5

	cpuUsed_pod_n1_ns2 = 2.0
	cpuUsed_pod_n2_ns2 = 2.0

	cpuUsed_pod1_n1_ns3 = 1.5
	cpuUsed_pod2_n1_ns3 = 1.0

	cpuUsed_pod_n2_ns3 = 0.0

	metricsSink               = metrics.NewEntityMetricSink()
	etype                     = metrics.PodType
	metric_cpuUsed_pod_ns1_n1 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns1_n1), metrics.CPU, metrics.Used, cpuUsed_pod_n1_ns1)
	metric_cpuUsed_pod_ns1_n2 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns1_n2), metrics.CPU, metrics.Used, cpuUsed_pod_n2_ns1)
	metric_cpuUsed_pod_ns2_n1 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns2_n1), metrics.CPU, metrics.Used, cpuUsed_pod_n1_ns2)
	metric_cpuUsed_pod_ns2_n2 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns2_n2), metrics.CPU, metrics.Used, cpuUsed_pod_n2_ns2)

	metric_cpuUsed_pod1_ns3_n1 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod1_ns3_n1), metrics.CPU, metrics.Used, cpuUsed_pod1_n1_ns3)
	metric_cpuUsed_pod2_ns3_n1 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod2_ns3_n1), metrics.CPU, metrics.Used, cpuUsed_pod2_n1_ns3)

	metric_cpuCap_pod_ns1_n1  = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns1_n1), metrics.CPU, metrics.Capacity, nodeCpuCap)
	metric_cpuCap_pod_ns1_n2  = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns1_n2), metrics.CPU, metrics.Capacity, nodeCpuCap)
	metric_cpuCap_pod_ns2_n1  = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns2_n1), metrics.CPU, metrics.Capacity, nodeCpuCap)
	metric_cpuCap_pod_ns2_n2  = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns2_n2), metrics.CPU, metrics.Capacity, nodeCpuCap)
	metric_cpuCap_pod1_ns3_n1 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod1_ns3_n1), metrics.CPU, metrics.Capacity, nodeCpuCap)
	metric_cpuCap_pod2_ns3_n1 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod2_ns3_n1), metrics.CPU, metrics.Capacity, nodeCpuCap)

	nodetype         = metrics.NodeType
	metric_cpuCap_n1 = metrics.NewEntityResourceMetric(nodetype, util.NodeKeyFunc(n1), metrics.CPU, metrics.Capacity, nodeCpuCap)
	metric_memCap_n1 = metrics.NewEntityResourceMetric(nodetype, util.NodeKeyFunc(n1), metrics.Memory, metrics.Capacity, nodeMemCap)
	metric_cpuCap_n2 = metrics.NewEntityResourceMetric(nodetype, util.NodeKeyFunc(n2), metrics.CPU, metrics.Capacity, nodeCpuCap)
	metric_memCap_n2 = metrics.NewEntityResourceMetric(nodetype, util.NodeKeyFunc(n2), metrics.Memory, metrics.Capacity, nodeMemCap)

	// CPU used for node n1 from pods
	cpuUsed_pod_n1 = cpuUsed_pod_n1_ns1 + cpuUsed_pod_n1_ns2 + cpuUsed_pod1_n1_ns3 + cpuUsed_pod2_n1_ns3
	cpuUsed_pod_n2 = cpuUsed_pod_n2_ns1 + cpuUsed_pod_n2_ns2

	nodeToPodsMap = map[string][]*v1.Pod{
		node1: {pod_ns1_n1, pod_ns2_n1, pod1_ns3_n1, pod2_ns3_n1},
		node2: {pod_ns1_n1, pod_ns2_n1},
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

	quotaToPodUsageMap = map[string]map[string]float64{
		ns1: {node1: cpuUsed_pod_n1_ns1,
			node2: cpuUsed_pod_n2_ns1},
		ns2: {node1: cpuUsed_pod_n1_ns2,
			node2: cpuUsed_pod_n2_ns2},
		ns3: {node1: cpuUsed_pod1_n1_ns3 + cpuUsed_pod2_n1_ns3,
			node2: 0.0},
		ns4: {node1: 0.0,
			node2: 0.0},
	}

	quotaToPodUsageSoldMap = map[string]float64{
		ns1: cpuUsed_pod_n1_ns1 + cpuUsed_pod_n2_ns1,
		ns2: cpuUsed_pod_n1_ns2 + cpuUsed_pod_n2_ns2,
		ns3: cpuUsed_pod1_n1_ns3 + cpuUsed_pod2_n1_ns3,
		ns4: 0.0,
	}

	nodeToPodCpuUsageMap = map[string]float64{
		node1: cpuUsed_pod_n1,
		node2: cpuUsed_pod_n2,
	}
)

func TestPodMetricsListAllocationUsage(t *testing.T) {
	metricsSink.AddNewMetricEntries(metric_cpuUsed_pod_ns1_n1, metric_cpuUsed_pod_ns1_n2)
	pm1 := createPodMetrics(pod_ns1_n1, ns1, metricsSink)
	pm2 := createPodMetrics(pod_ns1_n2, ns1, metricsSink)

	var podMetricsList PodMetricsList
	podMetricsList = append(podMetricsList, pm1)
	podMetricsList = append(podMetricsList, pm2)

	resourceMap := podMetricsList.SumAllocationUsage()
	cpuUsed := metric_cpuUsed_pod_ns1_n1.GetValue().(float64) + metric_cpuUsed_pod_ns1_n2.GetValue().(float64)
	memUsed := 0.0
	assert.Equal(t, cpuUsed, resourceMap[metrics.CPULimit])
	assert.Equal(t, memUsed, resourceMap[metrics.MemoryLimit])
	assert.Equal(t, cpuUsed, resourceMap[metrics.CPURequest])
	assert.Equal(t, memUsed, resourceMap[metrics.MemoryRequest])
}

func TestSumPodMetricsMissingComputeUsage(t *testing.T) {

	metricsSink := metrics.NewEntityMetricSink()
	pm1 := createPodMetrics(pod_ns1_n1, ns1, metricsSink)
	pm2 := createPodMetrics(pod_ns1_n2, ns1, metricsSink)

	var podMetricsList PodMetricsList
	podMetricsList = append(podMetricsList, pm1)
	podMetricsList = append(podMetricsList, pm2)

	resourceMap := podMetricsList.SumAllocationUsage()
	for _, allocationType := range metrics.ComputeAllocationResources {
		assert.Equal(t, 0.0, resourceMap[allocationType])
	}
}

func TestPodMetrics(t *testing.T) {
	metricsSink := metrics.NewEntityMetricSink()
	metricsSink.AddNewMetricEntries(metric_cpuUsed_pod_ns1_n1)
	pm := createPodMetrics(pod_ns1_n1, ns1, metricsSink)

	assert.Equal(t, pm.AllocationBought[metrics.CPULimit], cpuUsed_pod_n1_ns1)
	assert.Equal(t, pm.AllocationBought[metrics.CPURequest], cpuUsed_pod_n1_ns1)
	assert.Equal(t, pm.AllocationBought[metrics.MemoryLimit], 0.0)
	assert.Equal(t, pm.AllocationBought[metrics.MemoryRequest], 0.0)
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

func TestPodMetricsCollectionUnknownQuota(t *testing.T) {
	quotaName := "Quota_Unknown"
	clusterSummary := repository.CreateClusterSummary(kubeCluster)
	pod1 := &v1.Pod{}
	pod1.ObjectMeta.Name = "pod1"
	pod1.ObjectMeta.Namespace = quotaName
	pod1.Spec.NodeName = node1

	pod2 := &v1.Pod{}
	pod2.ObjectMeta.Name = "pod2"
	pod2.ObjectMeta.Namespace = quotaName
	pod2.Spec.NodeName = node2

	collector := &MetricsCollector{
		Cluster:     clusterSummary,
		MetricsSink: metricsSink,
		PodList:     []*v1.Pod{pod1, pod2},
		NodeList:    []*v1.Node{n1},
	}

	podCollection, _ := collector.CollectPodMetrics()
	assert.Equal(t, len(podCollection), 0)
}

func TestPodMetricsCollectionSingleNode(t *testing.T) {
	// Sink contains cpu capacity and used for all pods on node1
	metricsSink.AddNewMetricEntries(metric_cpuUsed_pod_ns1_n1, metric_cpuUsed_pod_ns2_n1,
		metric_cpuUsed_pod1_ns3_n1, metric_cpuUsed_pod2_ns3_n1)

	metricsSink.AddNewMetricEntries(metric_cpuCap_pod_ns1_n1, metric_cpuCap_pod_ns2_n1,
		metric_cpuCap_pod1_ns3_n1, metric_cpuCap_pod2_ns3_n1)

	podCpuCapMap := map[string]float64{
		pod_ns1_n1.Name:  metric_cpuCap_pod_ns1_n1.GetValue().(float64),
		pod_ns2_n1.Name:  metric_cpuCap_pod_ns2_n1.GetValue().(float64),
		pod1_ns3_n1.Name: metric_cpuCap_pod1_ns3_n1.GetValue().(float64),
		pod2_ns3_n1.Name: metric_cpuCap_pod2_ns3_n1.GetValue().(float64),
	}

	// Set limits for ns1 and ns2 for CPU,
	// compute capacity for the pods in these namespaces will be changed to the quota limit value
	kubeQuota1.SetResourceCapacity(metrics.CPULimit, 3.0)
	kubeQuota2.SetResourceCapacity(metrics.CPULimit, 2.0)

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
				allocationMap := podMetrics.AllocationBought
				for _, allocationType := range metrics.ComputeAllocationResources {
					_, exists := allocationMap[allocationType]
					assert.True(t, exists)
				}
				quota := collector.Cluster.QuotaMap[podMetrics.QuotaName]
				quotaCpu, _ := quota.GetAllocationResource(metrics.CPULimit)
				computeCapMap := podMetrics.ComputeCapacity
				if quotaCpu.Capacity < podCpuCapMap[podMetrics.PodName] {
					// assert that the pod's compute metrics is changed to the
					// match the quota's compute limit metrics
					podCpuCap, exists := computeCapMap[metrics.CPU]
					assert.True(t, exists)
					assert.Equal(t, quotaCpu.Capacity, podCpuCap)
				} else {
					computeCapMap := podMetrics.ComputeCapacity
					_, exists := computeCapMap[metrics.CPU]
					assert.False(t, exists)
				}
				_, exists = computeCapMap[metrics.Memory]
				assert.False(t, exists)
			}
		}
	}
}

func TestCreateNodeMetricsMap(t *testing.T) {
	metricsSink.AddNewMetricEntries(metric_cpuCap_n1, metric_memCap_n1)

	// Pods are in different namespaces and with only CPU metrics
	metricsSink.AddNewMetricEntries(metric_cpuUsed_pod_ns1_n1, metric_cpuUsed_pod_ns2_n1,
		metric_cpuUsed_pod1_ns3_n1, metric_cpuUsed_pod2_ns3_n1)

	pm1 := createPodMetrics(pod_ns1_n1, ns1, metricsSink)
	pm2 := createPodMetrics(pod_ns2_n1, ns2, metricsSink)
	pm3 := createPodMetrics(pod1_ns3_n1, ns3, metricsSink)
	pm4 := createPodMetrics(pod2_ns3_n1, ns3, metricsSink)
	pmList := []*repository.PodMetrics{pm1, pm2, pm3, pm4}

	nm := createNodeMetrics(n1, pmList, metricsSink)
	assertNodeAllocationUsage(t, nm, node1)
	assertNodeAllocationCapacity(t, nm)
}

func TestCreateMetricsMapForNodeWithEmptyPodList(t *testing.T) {
	metricsSink.AddNewMetricEntries(metric_cpuCap_n1, metric_memCap_n1)

	// empty pod list for the node
	pmList := []*repository.PodMetrics{}
	nm := createNodeMetrics(n1, pmList, metricsSink)

	// allocation used map is not created
	assert.Equal(t, nm.AllocationUsed[metrics.CPULimit], 0.0)
	assert.Equal(t, nm.AllocationUsed[metrics.CPURequest], 0.0)
	assert.Equal(t, nm.AllocationUsed[metrics.MemoryLimit], 0.0)
	assert.Equal(t, nm.AllocationUsed[metrics.MemoryRequest], 0.0)

	// allocation capacity map is created
	assertNodeAllocationCapacity(t, nm)
}

func TestNodeMetricsCollectionMultipleNodes(t *testing.T) {
	clusterSummary := repository.CreateClusterSummary(kubeCluster)
	collector := &MetricsCollector{
		Cluster:     clusterSummary,
		MetricsSink: metricsSink,
		PodList:     nodeToPodsMap[node1], //only pods from node1, no pods on node2
		NodeList:    []*v1.Node{n1, n2},
	}

	// cpu capacity and used for node1 and node2
	metricsSink.AddNewMetricEntries(metric_cpuCap_n1, metric_memCap_n1)
	metricsSink.AddNewMetricEntries(metric_cpuCap_n2, metric_memCap_n2)
	// cpu used for all pods on n1
	metricsSink.AddNewMetricEntries(metric_cpuUsed_pod_ns1_n1, metric_cpuUsed_pod_ns2_n1,
		metric_cpuUsed_pod1_ns3_n1, metric_cpuUsed_pod2_ns3_n1)

	podCollection, err := collector.CollectPodMetrics()
	assert.Nil(t, err)
	nodeCollection := collector.CollectNodeMetrics(podCollection)
	// Assert that the node metrics is created for all the nodes in the collector
	n1Metrics, exists := nodeCollection[node1]
	assert.True(t, exists)
	n2Metrics, exists := nodeCollection[node2]
	assert.True(t, exists)

	// Assert that the node allocation capacity map is created for all nodes, even the ones without any running pods
	assertNodeAllocationCapacity(t, n1Metrics)
	assertNodeAllocationCapacity(t, n2Metrics)

	// Assert the node allocation usage values for nodes with pods
	assertNodeAllocationUsage(t, n1Metrics, node1)
	assert.Equal(t, n2Metrics.AllocationUsed[metrics.CPULimit], 0.0)   // no pods on n2
	assert.Equal(t, n2Metrics.AllocationUsed[metrics.CPURequest], 0.0) // no pods on n2
}

func assertNodeAllocationCapacity(t *testing.T, nm *repository.NodeMetrics) {
	assert.Equal(t, nm.AllocationCap[metrics.CPULimit], nodeCpuCap) // node allocation capacity is equal to the node's compute resources
	assert.Equal(t, nm.AllocationCap[metrics.CPURequest], nodeCpuCap)
	assert.Equal(t, nm.AllocationCap[metrics.MemoryLimit], nodeMemCap) // node allocation capacity is equal to the node's compute resources
	assert.Equal(t, nm.AllocationCap[metrics.MemoryRequest], nodeMemCap)
}

func assertNodeAllocationUsage(t *testing.T, nm *repository.NodeMetrics, node string) {
	assert.Equal(t, nm.AllocationUsed[metrics.CPULimit], nodeToPodCpuUsageMap[node]) // node allocation capacity is equal to the node's compute resources
	assert.Equal(t, nm.AllocationUsed[metrics.CPURequest], nodeToPodCpuUsageMap[node])
	assert.Equal(t, nm.AllocationUsed[metrics.MemoryLimit], 0.0) // node allocation capacity is equal to the node's compute resources
	assert.Equal(t, nm.AllocationUsed[metrics.MemoryRequest], 0.0)
}

func TestQuotaMetricsMapAllNodes(t *testing.T) {
	clusterSummary := repository.CreateClusterSummary(kubeCluster)

	collector := &MetricsCollector{
		Cluster:     clusterSummary,
		MetricsSink: metricsSink,
		PodList:     []*v1.Pod{pod_ns1_n1, pod_ns1_n2, pod_ns2_n1, pod_ns2_n2, pod1_ns3_n1, pod2_ns3_n1},
		NodeList:    []*v1.Node{n1, n2},
	}

	metricsSink.AddNewMetricEntries(metric_cpuUsed_pod_ns1_n1, metric_cpuUsed_pod_ns1_n2,
		metric_cpuUsed_pod_ns2_n1, metric_cpuUsed_pod_ns2_n2,
		metric_cpuUsed_pod1_ns3_n1, metric_cpuUsed_pod2_ns3_n1)

	podMetricsMap, _ := collector.CollectPodMetrics()

	quotaMetricsList := collector.CollectQuotaMetrics(podMetricsMap)
	quotaMetricsMap := make(map[string]*repository.QuotaMetrics)
	for _, qm := range quotaMetricsList {
		quotaMetricsMap[qm.QuotaName] = qm
	}

	for quota, _ := range quotaToPodsMap {
		// Assert that the quota metrics map is created for all quotas in the cluster
		qm, exists := quotaMetricsMap[quota]
		assert.True(t, exists)
		qmMap := qm.AllocationBoughtMap
		// Assert that the allocation bought map is created for each node handled by the metrics collector
		assert.NotNil(t, qmMap[node1])
		assert.NotNil(t, qmMap[node2])

		cpuUsedOnNode1 := qmMap[node1][metrics.CPULimit]
		expectedCpuUsedOnNode1 := quotaToPodUsageMap[qm.QuotaName][node1]
		assert.Equal(t, expectedCpuUsedOnNode1, cpuUsedOnNode1)

		cpuUsedOnNode2 := qmMap[node2][metrics.CPULimit]
		expectedCpuUsedOnNode2 := quotaToPodUsageMap[qm.QuotaName][node2]
		assert.Equal(t, expectedCpuUsedOnNode2, cpuUsedOnNode2)

		qmSoldMap := qm.AllocationSold
		cpuUsedSold := qmSoldMap[metrics.CPULimit]
		expectedCpuUsedSold := quotaToPodUsageSoldMap[qm.QuotaName]
		assert.Equal(t, expectedCpuUsedSold, cpuUsedSold)
	}
}

func TestQuotaMetricsMapSingleNodeNoPods(t *testing.T) {
	clusterSummary := repository.CreateClusterSummary(kubeCluster)

	collector := &MetricsCollector{
		Cluster:     clusterSummary,
		MetricsSink: metricsSink,
		PodList:     []*v1.Pod{},
		NodeList:    []*v1.Node{n1}, // Metrics collector handles only one one node in the cluster
	}

	podMetricsMap, _ := collector.CollectPodMetrics()

	quotaMetricsList := collector.CollectQuotaMetrics(podMetricsMap)
	quotaMetricsMap := make(map[string]*repository.QuotaMetrics)
	for _, qm := range quotaMetricsList {
		quotaMetricsMap[qm.QuotaName] = qm
	}

	for quota, _ := range quotaToPodsMap {
		// Assert that the quota metrics map is created for all quotas in the cluster
		qm, exists := quotaMetricsMap[quota]
		assert.True(t, exists)
		// Assert that the allocation bought map is created only for the node handled by the metrics collector
		qmMap := qm.AllocationBoughtMap
		assert.NotNil(t, qmMap[node1])
		assert.Nil(t, qmMap[node2])
		assert.Equal(t, qmMap[node1][metrics.CPULimit], 0.0)
	}
}

func TestQuotaMetricsCpuUsage(t *testing.T) {
	nodeFreq := 2663.778000
	metric_cpuFreq_n1 := metrics.NewEntityStateMetric(nodetype, util.NodeKeyFunc(n1), metrics.CpuFrequency, nodeFreq)
	metricsSink.AddNewMetricEntries(metric_cpuFreq_n1)

	metricsSink.AddNewMetricEntries(metric_cpuUsed_pod_ns1_n1, metric_cpuUsed_pod_ns1_n2,
		metric_cpuUsed_pod_ns2_n1, metric_cpuUsed_pod_ns2_n2,
		metric_cpuUsed_pod1_ns3_n1, metric_cpuUsed_pod2_ns3_n1)

	clusterSummary := repository.CreateClusterSummary(kubeCluster)

	collector := &MetricsCollector{
		Cluster:     clusterSummary,
		MetricsSink: metricsSink,
		PodList:     nodeToPodsMap[node1],
		NodeList:    []*v1.Node{n1},
	}

	podMetricsMap, _ := collector.CollectPodMetrics()

	quotaMetricsList := collector.CollectQuotaMetrics(podMetricsMap)
	quotaMetricsMap := make(map[string]*repository.QuotaMetrics)
	for _, qm := range quotaMetricsList {
		quotaMetricsMap[qm.QuotaName] = qm
	}

	for quota, _ := range quotaToPodsMap {
		qm, _ := quotaMetricsMap[quota]
		// Assert that the allocation bought map is created only for the node handled by the metrics collector
		qmMap := qm.AllocationBoughtMap
		assert.NotNil(t, qmMap[node1])
		cpuLimit := qmMap[node1][metrics.CPULimit]
		expectedCpuLimit := quotaToPodUsageMap[qm.QuotaName][node1] * nodeFreq
		assert.Equal(t, cpuLimit, expectedCpuLimit)
	}
}
