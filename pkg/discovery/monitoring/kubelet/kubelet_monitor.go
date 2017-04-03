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
)

const (
	nanoToUnit int64 = 1E9

	byteToKiloBytes int64 = 1024
)

type KubeletMonitor struct {
	nodeList      []api.Node
	kubeletClient kubeletClient

	metricSink *metrics.MetricSink

	wg sync.WaitGroup
}

func NewKubeletMonitor(config KubeletMonitorConfig) (*KubeletMonitor, error) {
	kubeletClient, err := NewKubeletClient(config.KubeletClientConfig)
	if err != nil {
		return nil, fmt.Errorf("Failed to create Kubelet client based on given config: %s", err)
	}

	return &KubeletMonitor{
		kubeletClient: kubeletClient,
	}, nil
}

func (m *KubeletMonitor) InitMetricSink(sink *metrics.MetricSink) {
	m.metricSink = sink
}

func (m *KubeletMonitor) AssignTask(task *task.WorkerTask) {
	m.nodeList = task.NodeList()
}

// Start to retrieve resource stats for the received list of nodes.
func (m *KubeletMonitor) RetrieveResourceStat() {
	if m.nodeList == nil || len(m.nodeList) == 0 {
		// TODO, more informative.
		glog.Warning("Invalid nodeList or empty nodeList. Finish Immediately...")
		return
	}
	m.wg.Add(len(m.nodeList))

	for _, node := range m.nodeList {
		// TODO, do we want to add timeout?
		go m.scrapeKubelet(node)
	}

	m.wg.Wait()
}

// Retrieve resource metrics for the given node.
func (m *KubeletMonitor) scrapeKubelet(node *api.Node) {
	defer m.wg.Done()

	ip, err := getNodeIP(node)
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
	nodeMetrics := metrics.NewResourceMetric()
	// cpu
	cpuUsageCore := float64(*nodeStats.CPU.UsageNanoCores) / float64(nanoToUnit)
	nodeMetrics.SetMetric(metrics.CPU, metrics.Used, cpuUsageCore)
	// memory
	memoryUsageKiloBytes := float64(*nodeStats.Memory.UsageBytes) / float64(byteToKiloBytes)
	nodeMetrics.SetMetric(metrics.Memory, metrics.Used, memoryUsageKiloBytes)

	m.metricSink.AddNewMetricEntry(task.NodeType, util.NodeStatsKeyFunc(nodeStats), nodeMetrics)
}

// Parse pod stats for every pod and put them into sink.
func (m *KubeletMonitor) parsePodStats(podStats []stats.PodStats) {
	for _, podStat := range podStats {
		podMetrics := metrics.NewResourceMetric()
		var cpuUsageNanoCoreSum uint64
		var memoryUsageBytesSum uint64
		for _, containerStat := range podStat.Containers {
			cpuUsageNanoCoreSum += *containerStat.CPU.UsageNanoCores
			memoryUsageBytesSum += *containerStat.Memory.UsageBytes
		}
		podMetrics.SetMetric(metrics.CPU, metrics.Used, float64(cpuUsageNanoCoreSum)/float64(nanoToUnit))
		podMetrics.SetMetric(metrics.Memory, metrics.Used, float64(memoryUsageBytesSum)/float64(byteToKiloBytes))

		m.metricSink.AddNewMetricEntry(task.PodType, util.PodStatsKeyFunc(podStat), podMetrics)
	}
}

// Get the IP address for building kubelet client the given node.
func getNodeIP(node *api.Node) (string, error) {
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
