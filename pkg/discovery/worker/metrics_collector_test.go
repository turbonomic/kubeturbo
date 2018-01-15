package worker

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/pkg/api/v1"
	"testing"
)

func TestPodMetricsListAllocationUsage(t *testing.T) {

	var podMetricsList PodMetricsList
	allocation1 := map[metrics.ResourceType]float64{
		metrics.CPU:    4.0,
		metrics.Memory: 4,
	}
	allocation2 := map[metrics.ResourceType]float64{
		metrics.CPU:    6.0,
		metrics.Memory: 6,
	}
	podMetrics1 := &repository.PodMetrics{
		PodName:     "pod1",
		ComputeUsed: allocation1,
	}
	podMetrics2 := &repository.PodMetrics{
		PodName:     "pod1", //TODO:
		ComputeUsed: allocation2,
	}

	podMetricsList = append(podMetricsList, podMetrics1)
	podMetricsList = append(podMetricsList, podMetrics2)

	resourceMap := podMetricsList.SumAllocationUsage()
	assert.Equal(t, 10.0, resourceMap[metrics.CPULimit])
	assert.Equal(t, 10.0, resourceMap[metrics.MemoryLimit])
	assert.Equal(t, 10.0, resourceMap[metrics.CPURequest])
	assert.Equal(t, 10.0, resourceMap[metrics.MemoryRequest])
}

type MockPod struct {
	*v1.Pod
}

type MockSink struct {
	mock.Mock
}

var (
	node1 = "node1"
	node2 = "node2"
	ns1   = "ns1"
	ns2   = "ns2"
	ns3   = "ns3" // all pods on one node
	ns4   = "ns4" // no pods

	n1 = &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node1,
			UID:  types.UID(node1),
			// Resources TODO:
		},
	}

	n2 = &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node2,
			UID:  types.UID(node2),
			// Resources TODO:
		},
	}

	pod_ns1_n1 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod11",
			Namespace: ns1,
		},
		Spec: v1.PodSpec{
			NodeName: node1,
		},
	}

	pod_ns1_n2 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod12",
			Namespace: ns1,
		},
		Spec: v1.PodSpec{
			NodeName: node2,
		},
	}

	pod_ns2_n1 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod21",
			Namespace: ns2,
		},
		Spec: v1.PodSpec{
			NodeName: node1,
		},
	}

	pod_ns2_n2 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod22",
			Namespace: ns2,
		},
		Spec: v1.PodSpec{
			NodeName: node2,
		},
	}

	pod1_ns3_n1 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod31",
			Namespace: ns3,
		},
		Spec: v1.PodSpec{
			NodeName: node1,
		},
	}

	pod2_ns3_n1 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod32",
			Namespace: ns3,
		},
		Spec: v1.PodSpec{
			NodeName: node1,
		},
	}

	kubeNode1 = repository.NewKubeNode(n1, "cluster1")
	kubeNode2 = repository.NewKubeNode(n2, "cluster1")

	clusterResources map[metrics.ResourceType]*repository.KubeDiscoveredResource
	kubeQuota1       = repository.CreateDefaultQuota("cluster1", ns1, clusterResources)
	kubeQuota2       = repository.CreateDefaultQuota("cluster1", ns2, clusterResources)
	kubeQuota3       = repository.CreateDefaultQuota("cluster1", ns3, clusterResources)
	kubeQuota4       = repository.CreateDefaultQuota("cluster1", ns4, clusterResources)

	kubens1 = &repository.KubeNamespace{
		ClusterName: "cluster1",
		Name:        ns1,
		Quota:       kubeQuota1,
	}

	kubens2 = &repository.KubeNamespace{
		ClusterName: "cluster1",
		Name:        ns2,
		Quota:       kubeQuota2,
	}

	kubens3 = &repository.KubeNamespace{
		ClusterName: "cluster1",
		Name:        ns3,
		Quota:       kubeQuota3,
	}

	kubens4 = &repository.KubeNamespace{
		ClusterName: "cluster1",
		Name:        ns4,
		Quota:       kubeQuota4,
	}

	kubeCluster = &repository.KubeCluster{
		Name: "cluster1",
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
	cpuUsed_pod_n1_ns1 = 2.0
	cpuUsed_pod_n1_ns2 = 2.0
	cpuUsed_pod_n1_ns3 = 2.0
	cpuUsed_pod_n2_ns1 = 2.0
	cpuUsed_pod_n2_ns2 = 2.0
	cpuUsed_pod_n2_ns3 = 0.0

	metricsSink                = metrics.NewEntityMetricSink()
	etype                      = metrics.PodType
	metric_cpuUsed_pod_ns1_n1  = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns1_n1), metrics.CPU, metrics.Used, cpuUsed_pod_n1_ns1) //2.0)
	metric_cpuUsed_pod_ns1_n2  = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns1_n2), metrics.CPU, metrics.Used, cpuUsed_pod_n2_ns1)
	metric_cpuUsed_pod_ns2_n1  = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns2_n1), metrics.CPU, metrics.Used, cpuUsed_pod_n1_ns2)
	metric_cpuUsed_pod_ns2_n2  = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod_ns2_n2), metrics.CPU, metrics.Used, cpuUsed_pod_n2_ns2)
	metric_cpuUsed_pod1_ns3_n1 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod1_ns3_n1), metrics.CPU, metrics.Used, cpuUsed_pod_n1_ns3/2)
	metric_cpuUsed_pod2_ns3_n1 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod2_ns3_n1), metrics.CPU, metrics.Used, cpuUsed_pod_n1_ns3/2)

	cpuUsed_pod_n1 = cpuUsed_pod_n1_ns1 + cpuUsed_pod_n1_ns2 + cpuUsed_pod_n1_ns3
)

func TestPodMetrics(t *testing.T) {
	quotaName := "Quota1"
	metricsSink.AddNewMetricEntries(metric_cpuUsed_pod_ns1_n1)

	pm := createPodMetrics(pod_ns1_n1, quotaName, metricsSink)
	fmt.Printf("pod metrics %++v\n", pm.AllocationBought)
	assert.Equal(t, pm.AllocationBought[metrics.CPULimit], cpuUsed_pod_n1_ns1)
	assert.Equal(t, pm.AllocationBought[metrics.CPURequest], cpuUsed_pod_n1_ns1)

	assert.Equal(t, pm.AllocationBought[metrics.MemoryLimit], 0.0)
	assert.Equal(t, pm.AllocationBought[metrics.MemoryRequest], 0.0)
}

func TestCreateNodeMetricsMap(t *testing.T) {
	//allocation cap
	//allocation used
	etype := metrics.NodeType
	nodeKey := util.NodeKeyFunc(n1)

	cpuCap := metrics.NewEntityResourceMetric(etype, nodeKey, metrics.CPU, metrics.Capacity, 4.0)
	metricsSink.AddNewMetricEntries(cpuCap)
	cpuUsed := metrics.NewEntityResourceMetric(etype, nodeKey, metrics.CPU, metrics.Used, 2.0)
	metricsSink.AddNewMetricEntries(cpuUsed)

	etype = metrics.PodType

	// Pods in two different namespaces and with only CPU metrics
	quotaName := "Quota1"
	metricsSink.AddNewMetricEntries(metric_cpuUsed_pod_ns1_n1)
	pm1 := createPodMetrics(pod_ns1_n1, quotaName, metricsSink)

	quotaName = "Quota2"
	metricsSink.AddNewMetricEntries(metric_cpuUsed_pod_ns2_n1)
	pm2 := createPodMetrics(pod_ns2_n1, quotaName, metricsSink)

	quotaName = "Quota3"
	metricsSink.AddNewMetricEntries(metric_cpuUsed_pod1_ns3_n1)
	pm3 := createPodMetrics(pod1_ns3_n1, quotaName, metricsSink)
	metricsSink.AddNewMetricEntries(metric_cpuUsed_pod2_ns3_n1)
	pm4 := createPodMetrics(pod2_ns3_n1, quotaName, metricsSink)

	pmList := []*repository.PodMetrics{pm1, pm2, pm3, pm4}
	nm := createNodeMetrics(n1, pmList, metricsSink)
	assert.Equal(t, nm.AllocationUsed[metrics.CPULimit], cpuUsed_pod_n1) // used is sum of pod usages
	assert.Equal(t, nm.AllocationUsed[metrics.CPURequest], cpuUsed_pod_n1)
	assert.Equal(t, nm.AllocationUsed[metrics.MemoryLimit], 0.0) // pod usages is not available
	assert.Equal(t, nm.AllocationUsed[metrics.MemoryRequest], 0.0)

	assert.Equal(t, nm.AllocationCap[metrics.CPULimit], 4.0)
	assert.Equal(t, nm.AllocationCap[metrics.CPURequest], 4.0)
}

func TestCreateMetricsMapForNodeWithEmptyPodList(t *testing.T) {
	//allocation cap
	//allocation used
	etype := metrics.NodeType
	nodeKey := util.NodeKeyFunc(n1)

	cpuCap := metrics.NewEntityResourceMetric(etype, nodeKey, metrics.CPU, metrics.Capacity, 4.0)
	metricsSink.AddNewMetricEntries(cpuCap)
	cpuUsed := metrics.NewEntityResourceMetric(etype, nodeKey, metrics.CPU, metrics.Used, 2.0)
	metricsSink.AddNewMetricEntries(cpuUsed)

	etype = metrics.PodType

	// emoty pod list for the node
	pmList := []*repository.PodMetrics{}
	nm := createNodeMetrics(n1, pmList, metricsSink)
	assert.Equal(t, nm.AllocationUsed[metrics.CPULimit], 0.0)
	assert.Equal(t, nm.AllocationUsed[metrics.CPURequest], 0.0)
	assert.Equal(t, nm.AllocationUsed[metrics.MemoryLimit], 0.0)
	assert.Equal(t, nm.AllocationUsed[metrics.MemoryRequest], 0.0)

	assert.Equal(t, nm.AllocationCap[metrics.CPULimit], 4.0)
	assert.Equal(t, nm.AllocationCap[metrics.CPURequest], 4.0)
}

func TestQuotaMetricsMapAllNodes(t *testing.T) {
	clusterSummary := repository.CreateClusterSummary(kubeCluster)

	collector := &MetricsCollector{
		Cluster:     clusterSummary,
		MetricsSink: metricsSink,
		PodList:     []*v1.Pod{pod_ns1_n1, pod_ns1_n2, pod_ns2_n1, pod_ns2_n2, pod1_ns3_n1, pod1_ns3_n1},
		NodeList:    []*v1.Node{n1, n2},
	}

	metricsSink.AddNewMetricEntries(metric_cpuUsed_pod_ns1_n1, metric_cpuUsed_pod_ns1_n2,
		metric_cpuUsed_pod_ns2_n1, metric_cpuUsed_pod_ns2_n2,
		metric_cpuUsed_pod1_ns3_n1, metric_cpuUsed_pod2_ns3_n1)
	podMetricsMap := collector.CollectPodMetrics()

	nodeMetricsMap := collector.CollectNodeMetrics(podMetricsMap)
	nm1 := nodeMetricsMap[node1]
	assert.Equal(t, nm1.AllocationUsed[metrics.CPULimit], 6.0)
	nm2 := nodeMetricsMap[node2]
	assert.Equal(t, nm2.AllocationUsed[metrics.CPULimit], 4.0)

	var qm1, qm2, qm3, qm4 *repository.QuotaMetrics
	quotaMetricsMap := collector.CollectQuotaMetrics(podMetricsMap)

	for _, qm := range quotaMetricsMap {
		if qm.QuotaName == ns1 {
			qm1 = qm
		}
		if qm.QuotaName == ns3 {
			qm3 = qm
		}
		if qm.QuotaName == ns2 {
			qm2 = qm
		}
		if qm.QuotaName == ns4 {
			qm4 = qm
		}
	}
	// Assert that the quota metrics map is created for all quotas in the cluster
	// Assert that the allocation bought map is created for each node handled by the metrics collector
	assert.NotNil(t, qm1)
	qm1Map := qm1.AllocationBoughtMap
	assert.NotNil(t, qm1Map[node1])
	assert.NotNil(t, qm1Map[node2])
	assert.Equal(t, qm1Map[node1][metrics.CPULimit], cpuUsed_pod_n1_ns1)
	assert.Equal(t, qm1Map[node2][metrics.CPULimit], cpuUsed_pod_n2_ns1)

	assert.NotNil(t, qm2)
	qm2Map := qm2.AllocationBoughtMap
	assert.NotNil(t, qm2Map[node1])
	assert.NotNil(t, qm2Map[node2])
	assert.Equal(t, qm2Map[node1][metrics.CPULimit], cpuUsed_pod_n1_ns2)
	assert.Equal(t, qm2Map[node2][metrics.CPULimit], cpuUsed_pod_n2_ns2)

	assert.NotNil(t, qm3)
	qm3Map := qm3.AllocationBoughtMap
	assert.NotNil(t, qm3Map[node1])
	assert.NotNil(t, qm3Map[node2])
	assert.Equal(t, qm3Map[node1][metrics.CPULimit], cpuUsed_pod_n1_ns3)
	assert.Equal(t, qm3Map[node2][metrics.CPULimit], cpuUsed_pod_n2_ns3)

	assert.NotNil(t, qm4)
	qm4Map := qm4.AllocationBoughtMap
	assert.NotNil(t, qm4Map[node1])
	assert.NotNil(t, qm4Map[node2])
	assert.Equal(t, qm4Map[node1][metrics.CPULimit], 0.0)
	assert.Equal(t, qm4Map[node2][metrics.CPULimit], 0.0)
}

func TestQuotaMetricsMapSingleNode(t *testing.T) {
	clusterSummary := repository.CreateClusterSummary(kubeCluster)

	collector := &MetricsCollector{
		Cluster:     clusterSummary,
		MetricsSink: metricsSink,
		PodList:     []*v1.Pod{},
		NodeList:    []*v1.Node{n1}, // Metrics collector handles only one one node in the cluster
	}

	podMetricsMap := collector.CollectPodMetrics()

	var qm1, qm2, qm3, qm4 *repository.QuotaMetrics
	quotaMetricsMap := collector.CollectQuotaMetrics(podMetricsMap)

	for _, qm := range quotaMetricsMap {
		if qm.QuotaName == ns1 {
			qm1 = qm
		}
		if qm.QuotaName == ns3 {
			qm3 = qm
		}
		if qm.QuotaName == ns2 {
			qm2 = qm
		}
		if qm.QuotaName == ns4 {
			qm4 = qm
		}
	}

	// Assert that the quota metrics map is created for all quotas in the cluster
	// Assert that the allocation bought map is created only for the node handled by the metrics collector
	assert.NotNil(t, qm1)
	qm1Map := qm1.AllocationBoughtMap
	assert.NotNil(t, qm1Map[node1])
	assert.Equal(t, qm1Map[node1][metrics.CPULimit], 0.0)
	assert.Nil(t, qm1Map[node2])

	assert.NotNil(t, qm2)
	qm2Map := qm2.AllocationBoughtMap
	assert.NotNil(t, qm2Map[node1])
	assert.Equal(t, qm2Map[node1][metrics.CPULimit], 0.0)
	assert.Nil(t, qm2Map[node2])

	assert.NotNil(t, qm3)
	qm3Map := qm3.AllocationBoughtMap
	assert.NotNil(t, qm3Map[node1])
	assert.Equal(t, qm3Map[node1][metrics.CPULimit], 0.0)
	assert.Nil(t, qm3Map[node2])

	assert.NotNil(t, qm4)
	qm4Map := qm4.AllocationBoughtMap
	assert.NotNil(t, qm4Map[node1])
	assert.Equal(t, qm4Map[node1][metrics.CPULimit], 0.0)
	assert.Nil(t, qm4Map[node2])
}
