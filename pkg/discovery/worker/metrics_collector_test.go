package worker

import (
	"testing"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"fmt"
	"k8s.io/client-go/pkg/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestPodMetricsListAllocationUsage(t *testing.T) {

	var podMetricsList PodMetricsList
	allocation1 := map[metrics.ResourceType]float64{
		metrics.CPULimit: 4.0,
		metrics.MemoryLimit: 4,
	}
	allocation2 := map[metrics.ResourceType]float64{
		metrics.CPULimit: 6.0,
		metrics.MemoryLimit: 6,
	}
	podMetrics1 := &repository.PodMetrics {
		PodName: "pod1",
		AllocationUsed: allocation1,
	}
	podMetrics2 := &repository.PodMetrics {
		PodName: "pod1",
		AllocationUsed: allocation2,
	}

	podMetricsList = append(podMetricsList, podMetrics1)
	podMetricsList = append(podMetricsList, podMetrics2)

	resourceMap := podMetricsList.SumAllocationUsage()
	assert.Equal(t, 10.0, resourceMap[metrics.CPULimit])
	assert.Equal(t, 10.0, resourceMap[metrics.MemoryLimit])
	assert.Equal(t, 0.0, resourceMap[metrics.CPURequest])
	assert.Equal(t, 0.0, resourceMap[metrics.MemoryRequest])
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
	ns1 = "ns1"
	ns2 = "ns2"
	ns3 = "ns3"	// all pods on one node
	ns4 = "ns4"	// no pods

	n1 = &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node1,
			UID: types.UID(node1),
			// Resources TODO:
		},
	}

	n2 = &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: node2,
			UID: types.UID(node2),
			// Resources TODO:
		},
	}

	pod11 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod11",
			Namespace: ns1,
		},
		Spec: v1.PodSpec{
			NodeName: node1,
		},
	}

	pod12 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod12",
			Namespace: ns1,
		},
		Spec: v1.PodSpec{
			NodeName: node2,
		},
	}

	pod21 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod21",
			Namespace: ns2,
		},
		Spec: v1.PodSpec{
			NodeName: node1,
		},
	}

	pod22 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod22",
			Namespace: ns2,
		},
		Spec: v1.PodSpec{
			NodeName: node2,
		},
	}

	pod31 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod31",
			Namespace: ns3,
		},
		Spec: v1.PodSpec{
			NodeName: node1,
		},
	}

	pod32 = &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pod32",
			Namespace: ns3,
		},
		Spec: v1.PodSpec{
			NodeName: node1,
		},
	}

	kubeNode1 = &repository.KubeNode {
		Node: n1,
	}
	kubeNode2 = &repository.KubeNode {
		Node: n2,
	}

	kubeQuota1 = repository.NewKubeQuota("cluster1", ns1)
	kubeQuota2 = repository.NewKubeQuota("cluster1", ns2)
	kubeQuota3 = repository.NewKubeQuota("cluster1", ns3)
	kubeQuota4 = repository.NewKubeQuota("cluster1", ns4)

	kubens1 = &repository.KubeNamespace {
		ClusterName: "cluster1",
		Name: ns1,
		Quota: kubeQuota1,
	}

	kubens2 = &repository.KubeNamespace {
		ClusterName: "cluster1",
		Name: ns2,
		Quota: kubeQuota2,
	}

	kubens3 = &repository.KubeNamespace {
		ClusterName: "cluster1",
		Name: ns3,
		Quota: kubeQuota3,
	}

	kubens4 = &repository.KubeNamespace {
		ClusterName: "cluster1",
		Name: ns4,
		Quota: kubeQuota4,
	}

	kubeCluster = &repository.KubeCluster{
		Name: "cluster1",
		Nodes: map[string]*repository.KubeNode {
			node1: kubeNode1,
			node2: kubeNode2,
		},
		Namespaces: map[string]*repository.KubeNamespace {
			ns1: kubens1,
			ns2: kubens2,
			ns3: kubens3,
			ns4: kubens4,
		},
	}

	metricsSink = metrics.NewEntityMetricSink()
	etype = metrics.PodType
	cpuUsed_pod11 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod11), metrics.CPU, metrics.Used, 2.0)
	cpuUsed_pod12 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod12), metrics.CPU, metrics.Used, 2.0)
	cpuUsed_pod21 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod21), metrics.CPU, metrics.Used, 2.0)
	cpuUsed_pod22 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod22), metrics.CPU, metrics.Used, 2.0)
	cpuUsed_pod31 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod31), metrics.CPU, metrics.Used, 1.0)
	cpuUsed_pod32 = metrics.NewEntityResourceMetric(etype, util.PodKeyFunc(pod32), metrics.CPU, metrics.Used, 1.0)
)

func TestPodMetrics(t *testing.T) {

	etype := metrics.PodType
	podKey := util.PodKeyFunc(pod11)
	quotaName := "Quota1"

	cpuUsed := metrics.NewEntityResourceMetric(etype, podKey, metrics.CPU, metrics.Used, 2.0)
	metricsSink.AddNewMetricEntries(cpuUsed)

	pm := createPodMetrics(pod11, quotaName, metricsSink)
	fmt.Printf("pod metrics %++v\n", pm.AllocationUsed)
	assert.Equal(t, pm.AllocationUsed[metrics.CPULimit], 2.0)
	assert.Equal(t, pm.AllocationUsed[metrics.CPURequest], 2.0)
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

	podKey := util.PodKeyFunc(pod11)
	quotaName := "Quota1"
	cpuUsed = metrics.NewEntityResourceMetric(etype, podKey, metrics.CPU, metrics.Used, 1.5)
	metricsSink.AddNewMetricEntries(cpuUsed)
	pm1 := createPodMetrics(pod11, quotaName, metricsSink)

	podKey = util.PodKeyFunc(pod21)
	quotaName = "Quota2"
	cpuUsed = metrics.NewEntityResourceMetric(etype, podKey, metrics.CPU, metrics.Used, 1.5)
	metricsSink.AddNewMetricEntries(cpuUsed)
	pm2 := createPodMetrics(pod21, quotaName, metricsSink)

	pmList := []*repository.PodMetrics{pm1, pm2}
	nm := createNodeMetrics(n1, pmList, metricsSink)
	fmt.Printf("pod metrics %++v\n", nm.AllocationUsed)
	assert.Equal(t, nm.AllocationUsed[metrics.CPULimit], 3.0)
	assert.Equal(t, nm.AllocationUsed[metrics.CPURequest], 3.0)
	assert.Equal(t, nm.AllocationCap[metrics.CPULimit], 4.0)
	assert.Equal(t, nm.AllocationCap[metrics.CPURequest], 4.0)
}

func TestCreatePodMetricsMap(t *testing.T) {
	clusterSummary := repository.CreateClusterSummary(kubeCluster)

	collector := &MetricsCollector{
		Cluster: clusterSummary,
		MetricsSink: metricsSink,
		PodList: []*v1.Pod{pod11,pod12,pod21, pod22, pod31, pod32},
	}

	metricsSink.AddNewMetricEntries(cpuUsed_pod11, cpuUsed_pod12,
						cpuUsed_pod21, cpuUsed_pod22,
						cpuUsed_pod31, cpuUsed_pod32,)
	podMetricsMap := collector.CollectPodMetrics()
	for nodeName, quotaMap := range podMetricsMap {
		fmt.Printf("%s \n", nodeName)
		for quotaName, podList := range quotaMap {
			fmt.Printf("\t%s \n", quotaName)
			for _, pod := range podList {
				fmt.Printf("\t\t%s:%++v \n", pod.PodName, pod.AllocationUsed)
			}
		}
	}

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
			fmt.Printf("qm1 %++v\n", qm1)
		}
		if qm.QuotaName == ns3 {
			qm3 = qm
			fmt.Printf("qm3 %++v\n", qm3)
		}
		if qm.QuotaName == ns2 {
			qm2 = qm
			fmt.Printf("qm2 %++v\n", qm2)
		}
		if qm.QuotaName == ns4 {
			qm4 = qm
			fmt.Printf("qm4 %++v\n", qm4)
		}
	}
	qm1Map := qm1.AllocationBoughtMap
	assert.Equal(t, qm1Map[node1][metrics.CPULimit], 2.0)
	assert.Equal(t, qm1Map[node2][metrics.CPULimit], 2.0)

	qm2Map := qm2.AllocationBoughtMap
	assert.Equal(t, qm2Map[node1][metrics.CPULimit], 2.0)
	assert.Equal(t, qm2Map[node2][metrics.CPULimit], 2.0)

	qm3Map := qm3.AllocationBoughtMap
	assert.Equal(t, qm3Map[node1][metrics.CPULimit], 2.0)
	assert.Equal(t, qm3Map[node2][metrics.CPULimit], 0.0)

	assert.Nil(t, qm4)
}
