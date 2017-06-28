package kubelet

import (
	"errors"
	"fmt"
	"sync"

	api "k8s.io/client-go/pkg/api/v1"
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	cadvisorapi "github.com/google/cadvisor/info/v1"

	"github.com/golang/glog"
)

// KubeletMonitor is a resource monitoring worker.
type KubeletMonitor struct {
	nodeList []*api.Node

	kubeletClient *kubeletClient

	metricSink *metrics.EntityMetricSink

	wg sync.WaitGroup
}

func NewKubeletMonitor(config *KubeletMonitorConfig) (*KubeletMonitor, error) {
	kubeletClient, err := NewKubeletClient(config.KubeletClientConfig)
	if err != nil {
		return nil, fmt.Errorf("Failed to create Kubelet client based on given config: %s", err)
	}

	return &KubeletMonitor{
		kubeletClient: kubeletClient,
		metricSink:    metrics.NewEntityMetricSink(),
	}, nil
}

func (m *KubeletMonitor) GetMonitoringSource() types.MonitoringSource {
	return types.KubeletSource
}

func (m *KubeletMonitor) ReceiveTask(task *task.Task) {
	m.nodeList = task.NodeList()
}

func (m *KubeletMonitor) Do() *metrics.EntityMetricSink {
	err := m.RetrieveResourceStat()
	if err != nil {
		glog.Errorf("Failed to execute task: %s", err)
	}

	return m.metricSink
}

// Start to retrieve resource stats for the received list of nodes.
func (m *KubeletMonitor) RetrieveResourceStat() error {
	if m.nodeList == nil || len(m.nodeList) == 0 {
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

	ip, err := util.GetNodeIPForMonitor(node, types.KubeletSource)
	if err != nil {
		glog.Errorf("Failed to get resource metrics from %s: %s", node.Name, err)
		return
	}
	host := Host{
		IP:   ip,
		Port: m.kubeletClient.GetPort(),
	}
	// get machine information
	machineInfo, err := m.kubeletClient.GetMachineInfo(host)
	if err != nil {
		glog.Errorf("Failed to get machine information from %s: %s", node.Name, err)
		return
	}
	glog.V(4).Infof("Machine info of %s is %++v", node.Name, machineInfo)
	m.parseNodeInfo(node, machineInfo)

	// get summary information about the given node and the pods running on it.
	summary, err := m.kubeletClient.GetSummary(host)
	if err != nil {
		glog.Errorf("Failed to get resource metrics summary from %s: %s", node.Name, err)
		return
	}
	m.parseNodeStats(summary.Node)
	m.parsePodStats(summary.Pods)

	glog.Infof("Finished scrape node %s.", node.Name)

}

func (m *KubeletMonitor) parseNodeInfo(node *api.Node, machineInfo *cadvisorapi.MachineInfo) {
	cpuFrequencyMHz := float64(machineInfo.CpuFrequency) / util.MegaToKilo
	cpuFrequencyMetric := metrics.NewEntityStateMetric(task.NodeType, util.NodeKeyFunc(node), metrics.CpuFrequency, cpuFrequencyMHz)
	m.metricSink.AddNewMetricEntries(cpuFrequencyMetric)
}

// Parse node stats and put it into sink.
func (m *KubeletMonitor) parseNodeStats(nodeStats stats.NodeStats) {
	// cpu
	cpuUsageCore := float64(*nodeStats.CPU.UsageNanoCores) / util.NanoToUnit
	glog.V(4).Infof("Cpu usage of node %s is %f core", nodeStats.NodeName, cpuUsageCore)
	nodeCpuUsageCoreMetrics := metrics.NewEntityResourceMetric(task.NodeType, util.NodeStatsKeyFunc(nodeStats),
		metrics.CPU, metrics.Used, cpuUsageCore)

	// memory
	memoryUsageKiloBytes := float64(*nodeStats.Memory.UsageBytes) / util.KilobytesToBytes
	glog.V(4).Infof("Memory usage of node %s is %f Kb", nodeStats.NodeName, memoryUsageKiloBytes)
	nodeMemoryUsageKiloBytesMetrics := metrics.NewEntityResourceMetric(task.NodeType,
		util.NodeStatsKeyFunc(nodeStats), metrics.Memory, metrics.Used, memoryUsageKiloBytes)

	m.metricSink.AddNewMetricEntries(nodeCpuUsageCoreMetrics, nodeMemoryUsageKiloBytesMetrics)

}

// Parse pod stats for every pod and put them into sink.
func (m *KubeletMonitor) parsePodStats(podStats []stats.PodStats) {
	for _, podStat := range podStats {
		var cpuUsageNanoCoreSum uint64
		var memoryUsageBytesSum uint64
		for _, containerStat := range podStat.Containers {
			if containerStat.CPU != nil {
				cpuUsageNanoCoreSum += *containerStat.CPU.UsageNanoCores
			}
			if containerStat.Memory != nil {
				memoryUsageBytesSum += *containerStat.Memory.UsageBytes
			}
		}
		glog.V(4).Infof("Cpu usage of pod %s is %f core", util.PodStatsKeyFunc(podStat),
			float64(cpuUsageNanoCoreSum)/util.NanoToUnit)
		podCpuUsageCoreMetrics := metrics.NewEntityResourceMetric(task.PodType, util.PodStatsKeyFunc(podStat),
			metrics.CPU, metrics.Used, float64(cpuUsageNanoCoreSum)/util.NanoToUnit)

		glog.V(4).Infof("Memory usage of pod %s is %f Kb", util.PodStatsKeyFunc(podStat),
			float64(memoryUsageBytesSum)/util.KilobytesToBytes)
		podMemoryUsageCoreMetrics := metrics.NewEntityResourceMetric(task.PodType, util.PodStatsKeyFunc(podStat),
			metrics.Memory, metrics.Used, float64(memoryUsageBytesSum)/util.KilobytesToBytes)

		// application cpu and mem used are the same as pod's.
		applicationCpuUsageCoreMetrics := metrics.NewEntityResourceMetric(task.ApplicationType,
			util.PodStatsKeyFunc(podStat), metrics.CPU, metrics.Used,
			float64(cpuUsageNanoCoreSum)/util.NanoToUnit)
		applicationMemoryUsageCoreMetrics := metrics.NewEntityResourceMetric(task.ApplicationType,
			util.PodStatsKeyFunc(podStat), metrics.Memory, metrics.Used,
			float64(memoryUsageBytesSum)/util.KilobytesToBytes)

		m.metricSink.AddNewMetricEntries(podCpuUsageCoreMetrics,
			podMemoryUsageCoreMetrics,
			applicationCpuUsageCoreMetrics,
			applicationMemoryUsageCoreMetrics)
	}
}
