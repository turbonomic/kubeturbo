package kubelet

import (
	"errors"
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

	kubeletClient *KubeletClient

	metricSink *metrics.EntityMetricSink

	stopCh chan struct{}

	wg sync.WaitGroup
}

func NewKubeletMonitor(config *KubeletMonitorConfig) (*KubeletMonitor, error) {
	return &KubeletMonitor{
		kubeletClient: config.kubeletClient,
		metricSink:    metrics.NewEntityMetricSink(),
		stopCh:        make(chan struct{}, 1),
	}, nil
}

func (m *KubeletMonitor) reset() {
	m.metricSink = metrics.NewEntityMetricSink()
	m.stopCh = make(chan struct{}, 1)
}

func (m *KubeletMonitor) GetMonitoringSource() types.MonitoringSource {
	return types.KubeletSource
}

func (m *KubeletMonitor) ReceiveTask(task *task.Task) {
	m.reset()

	m.nodeList = task.NodeList()
}

func (m *KubeletMonitor) Stop() {
	m.stopCh <- struct{}{}
}

func (m *KubeletMonitor) Do() *metrics.EntityMetricSink {
	glog.V(4).Infof("%s has started task.", m.GetMonitoringSource())
	err := m.RetrieveResourceStat()
	if err != nil {
		glog.Errorf("Failed to execute task: %s", err)
	}
	glog.V(4).Infof("%s monitor has finished task.", m.GetMonitoringSource())
	return m.metricSink
}

// Start to retrieve resource stats for the received list of nodes.
func (m *KubeletMonitor) RetrieveResourceStat() error {
	defer func() {
		close(m.stopCh)
	}()
	if m.nodeList == nil || len(m.nodeList) == 0 {
		return errors.New("Invalid nodeList or empty nodeList. Finish Immediately...")
	}
	m.wg.Add(len(m.nodeList))

	for _, node := range m.nodeList {
		go func(n *api.Node) {
			defer m.wg.Done()
			select {
			case <-m.stopCh:
				return
			default:
				m.scrapeKubelet(n)
			}
		}(node)
	}

	m.wg.Wait()

	return nil
}

// Retrieve resource metrics for the given node.
func (m *KubeletMonitor) scrapeKubelet(node *api.Node) {
	ip, err := util.GetNodeIPForMonitor(node, types.KubeletSource)
	if err != nil {
		glog.Errorf("Failed to get resource metrics from %s: %s", node.Name, err)
		return
	}

	// get machine information
	machineInfo, err := m.kubeletClient.GetMachineInfo(ip)
	if err != nil {
		glog.Errorf("Failed to get machine information from %s: %s", node.Name, err)
		return
	}
	glog.V(4).Infof("Machine info of %s is %++v", node.Name, machineInfo)
	m.parseNodeInfo(node, machineInfo)

	// get summary information about the given node and the pods running on it.
	summary, err := m.kubeletClient.GetSummary(ip)
	if err != nil {
		glog.Errorf("Failed to get resource metrics summary from %s: %s", node.Name, err)
		return
	}
	m.parseNodeStats(summary.Node)
	m.parsePodStats(summary.Pods)

	glog.V(4).Infof("Finished scrape node %s.", node.Name)

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
			if containerStat.CPU != nil && containerStat.CPU.UsageNanoCores != nil {
				cpuUsageNanoCoreSum += *containerStat.CPU.UsageNanoCores
			}
			if containerStat.Memory != nil && containerStat.Memory.UsageBytes != nil {
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
