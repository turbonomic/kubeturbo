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

	stopCh chan struct{}

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

	glog.V(4).Infof("Finished scrape node %s.", node.Name)

}

func (m *KubeletMonitor) parseNodeInfo(node *api.Node, machineInfo *cadvisorapi.MachineInfo) {
	cpuFrequencyMHz := float64(machineInfo.CpuFrequency) / util.MegaToKilo
	glog.V(4).Infof("node-%s cpuFrequency = %.2fMHz", node.Name, cpuFrequencyMHz)
	cpuFrequencyMetric := metrics.NewEntityStateMetric(task.NodeType, util.NodeKeyFunc(node), metrics.CpuFrequency, cpuFrequencyMHz)
	m.metricSink.AddNewMetricEntries(cpuFrequencyMetric)
}

// Parse node stats and put it into sink.
func (m *KubeletMonitor) parseNodeStats(nodeStats stats.NodeStats) {
	// cpu
	cpuUsageCore := float64(*nodeStats.CPU.UsageNanoCores) / util.NanoToUnit
	memoryUsageKiloBytes := float64(*nodeStats.Memory.UsageBytes) / util.KilobytesToBytes

	key := util.NodeStatsKeyFunc(nodeStats)
	glog.V(3).Infof("CPU usage of node %s is %.3f core", nodeStats.NodeName, cpuUsageCore)
	glog.V(3).Infof("Memory usage of node %s is %.3f KB", nodeStats.NodeName, memoryUsageKiloBytes)
	m.genUsedMetrics(task.NodeType, key, cpuUsageCore, memoryUsageKiloBytes)
}

// Parse pod stats for every pod and put them into sink.
func (m *KubeletMonitor) parsePodStats(podStats []stats.PodStats) {
	for i := range podStats {
		pod := &(podStats[i])
		cpuUsed, memUsed := m.parseContainerStats(pod)

		key := util.PodStatsKeyFunc(pod)
		glog.V(4).Infof("Cpu usage of pod %s is %.3f core", key, cpuUsed)
		glog.V(4).Infof("Memory usage of pod %s is %.3f Kb", key, memUsed)

		m.genUsedMetrics(task.PodType, key, cpuUsed, memUsed)
	}
}

func (m *KubeletMonitor) parseContainerStats(pod *stats.PodStats) (float64, float64) {

	totalUsedCPU := float64(0.0)
	totalUsedMem := float64(0.0)

	podId := pod.PodRef.UID
	containers := pod.Containers

	for i := range containers {
		container := &containers[i]
		if container.CPU == nil || container.CPU.UsageNanoCores == nil {
			continue
		}
		if container.Memory == nil || container.Memory.UsageBytes == nil {
			continue
		}

		cpuUsed := float64(*(container.CPU.UsageNanoCores)) / util.NanoToUnit
		memUsed := float64(*(container.Memory.UsageBytes)) / util.KilobytesToBytes

		totalUsedCPU += cpuUsed
		totalUsedMem += memUsed

		//1. container Used
		containerId := util.ContainerIdFunc(podId, i)
		m.genUsedMetrics(task.ContainerType, containerId, cpuUsed, memUsed)

		containerName := fmt.Sprintf("%s-%s", pod.PodRef.Name, container.Name)
		glog.V(4).Infof("container[%s] cpu/memory usage:%.3f, %.3f", containerName, cpuUsed, memUsed)

		//2. app Used
		appId := util.ApplicationIdFunc(containerId)
		m.genUsedMetrics(task.ApplicationType, appId, cpuUsed, memUsed)
	}

	return totalUsedCPU, totalUsedMem
}

func (m *KubeletMonitor) genUsedMetrics(etype task.DiscoveredEntityType, key string, cpu, memory float64) {
	cpuMetric := metrics.NewEntityResourceMetric(etype, key, metrics.CPU, metrics.Used, cpu)
	memMetric := metrics.NewEntityResourceMetric(etype, key, metrics.Memory, metrics.Used, memory)
	m.metricSink.AddNewMetricEntries(cpuMetric, memMetric)
}
