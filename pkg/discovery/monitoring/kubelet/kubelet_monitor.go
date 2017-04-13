package kubelet

import (
	"fmt"
	"sync"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/stats"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	"github.com/golang/glog"
	"errors"
)



// KubeletMonitor is a resource monitoring worker.
type KubeletMonitor struct {
	nodeList []*api.Node

	kubeletClient *kubeletClient

	metricSink *metrics.EntityMetricSink

	wg sync.WaitGroup
}

func NewKubeletMonitor(config KubeletMonitorConfig) (*KubeletMonitor, error) {
	kubeletClient, err := NewKubeletClient(config.KubeletClientConfig)
	if err != nil {
		return nil, fmt.Errorf("Failed to create Kubelet client based on given config: %s", err)
	}

	return &KubeletMonitor{
		kubeletClient: kubeletClient,
		metricSink:    metrics.NewEntityMetricSink(),
	}, nil
}

func (m *KubeletMonitor) AssignTask(task *task.Task) {
	m.nodeList = task.NodeList()
}

func (m *KubeletMonitor) Do() *metrics.EntityMetricSink {
	m.RetrieveResourceStat()

	return m.metricSink
}

// Start to retrieve resource stats for the received list of nodes.
func (m *KubeletMonitor) RetrieveResourceStat() error{
	if m.nodeList == nil || len(m.nodeList) == 0 {
		// TODO, more informative.
		return errors.New("Invalid nodeList or empty nodeList. Finish Immediately...")
	}
	m.wg.Add(len(m.nodeList))

	for _, node := range m.nodeList {
		// TODO, do we want to add timeout?
		n := node
		go m.scrapeKubelet(n)
	}

	m.wg.Wait()

	return nil
}

// Retrieve resource metrics for the given node.
func (m *KubeletMonitor) scrapeKubelet(node *api.Node) {
	defer m.wg.Done()

	ip, err := getNodeIPForKubelet(node)
	if err != nil {
		glog.Errorf("Failed to get resource metrics from %s: %s", node.Name, err)
		return
	}
	host := Host{
		IP:   ip,
		Port: m.kubeletClient.GetPort(),
	}
	summary, err := m.kubeletClient.GetSummary(host)
	if err != nil {
		glog.Errorf("Failed to get resource metrics summary from %s: %s", node.Name, err)
		return
	}
	m.parseNodeStats(summary.Node)
	m.parsePodStats(summary.Pods)

}

// Parse node stats and put it into sink.
func (m *KubeletMonitor) parseNodeStats(nodeStats stats.NodeStats) {
	// cpu
	cpuUsageCore := float64(*nodeStats.CPU.UsageNanoCores) / float64(util.NanoToUnit)
	nodeCpuUsageCoreMetrics := metrics.NewEntityResourceMetric(task.NodeType, util.NodeStatsKeyFunc(nodeStats),
		metrics.CPU, metrics.Used, cpuUsageCore)
	m.metricSink.AddNewMetricEntry(nodeCpuUsageCoreMetrics)

	// memory
	memoryUsageKiloBytes := float64(*nodeStats.Memory.UsageBytes) / float64(util.ByteToKiloBytes)
	nodeMemoryUsageKiloBytesMetrics := metrics.NewEntityResourceMetric(task.NodeType,
		util.NodeStatsKeyFunc(nodeStats), metrics.Memory, metrics.Used, memoryUsageKiloBytes)
	m.metricSink.AddNewMetricEntry(nodeMemoryUsageKiloBytesMetrics)

}

// Parse pod stats for every pod and put them into sink.
func (m *KubeletMonitor) parsePodStats(podStats []stats.PodStats) {
	for _, podStat := range podStats {
		var cpuUsageNanoCoreSum uint64
		var memoryUsageBytesSum uint64
		for _, containerStat := range podStat.Containers {
			cpuUsageNanoCoreSum += *containerStat.CPU.UsageNanoCores
			memoryUsageBytesSum += *containerStat.Memory.UsageBytes
		}

		podCpuUsageCoreMetrics := metrics.NewEntityResourceMetric(task.PodType, util.PodStatsKeyFunc(podStat),
			metrics.CPU, metrics.Used, float64(cpuUsageNanoCoreSum)/float64(util.NanoToUnit))
		m.metricSink.AddNewMetricEntry(podCpuUsageCoreMetrics)

		podMemoryUsageCoreMetrics := metrics.NewEntityResourceMetric(task.PodType, util.PodStatsKeyFunc(podStat),
			metrics.Memory, metrics.Used, float64(memoryUsageBytesSum)/float64(util.ByteToKiloBytes))
		m.metricSink.AddNewMetricEntry(podMemoryUsageCoreMetrics)
	}
}

// Get the IP address for building kubelet client the given node.
func getNodeIPForKubelet(node *api.Node) (string, error) {
	hostname, ip := node.Name, ""
	for _, addr := range node.Status.Addresses {
		if addr.Type == api.NodeHostName && addr.Address != "" {
			hostname = addr.Address
		}
		if addr.Type == api.NodeInternalIP && addr.Address != "" {
			ip = addr.Address
		}
		if addr.Type == api.NodeLegacyHostIP && addr.Address != "" && ip == "" {
			ip = addr.Address
		}
	}
	if ip != "" {
		return ip, nil
	}
	return "", fmt.Errorf("Node %v has no valid hostname and/or IP address: %v %v", node.Name, hostname, ip)
}
