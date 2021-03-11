package kubelet

import (
	"errors"
	"fmt"
	"sync"
	"time"

	api "k8s.io/api/core/v1"
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/features"
	"k8s.io/client-go/kubernetes"
	evictionapi "k8s.io/kubernetes/pkg/kubelet/eviction/api"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"

	utilfeature "k8s.io/apiserver/pkg/util/feature"
)

// KubeletMonitor is a resource monitoring worker.
type KubeletMonitor struct {
	nodeList []*api.Node

	kubeletClient *kubeclient.KubeletClient

	// Backup k8s client for node cpufrequency
	kubeClient *kubernetes.Clientset

	metricSink *metrics.EntityMetricSink

	stopCh chan struct{}

	wg sync.WaitGroup

	// Whether this kubelet monitor runs during full discovery
	isFullDiscovery bool
}

func NewKubeletMonitor(config *KubeletMonitorConfig, isFullDiscovery bool) (*KubeletMonitor, error) {
	return &KubeletMonitor{
		kubeletClient:   config.kubeletClient,
		kubeClient:      config.kubeClient,
		metricSink:      metrics.NewEntityMetricSink(),
		stopCh:          make(chan struct{}, 1),
		isFullDiscovery: isFullDiscovery,
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
	kc := m.kubeletClient

	// Collect node cpu frequency metric only in full discovery not in sampling discovery
	if m.isFullDiscovery {
		nodefreq, err := kc.GetNodeCpuFrequency(node)
		if err != nil {
			glog.Errorf("Failed to get resource metrics (cpufreq) from %s: %s", node.Name, err)
			return
		}
		m.parseNodeCpuFreq(node, nodefreq)
	}

	ip, err := util.GetNodeIPForMonitor(node, types.KubeletSource)
	if err != nil {
		glog.Errorf("Failed to get resource metrics summary from %s: %s", node.Name, err)
		return
	}
	// get summary information about the given node and the pods running on it.
	summary, err := kc.GetSummary(ip, node.Name)
	if err != nil {
		glog.Errorf("Failed to get resource metrics summary from %s: %s", node.Name, err)
		return
	}
	// Indicate that we have used the cache last time we've asked for some of the info.
	if kc.HasCacheBeenUsed(ip) {
		if m.isFullDiscovery {
			cacheUsedMetric := metrics.NewEntityStateMetric(metrics.NodeType, util.NodeKeyFunc(node), "NodeCacheUsed", 1)
			m.metricSink.AddNewMetricEntries(cacheUsedMetric)
		} else {
			// It's a valid case if a node is available from the full discovery but not available during sampling discoveries.
			// Need to wait for a full discovery to fetch the available nodes.
			glog.Warningf("Failed to get resource metrics summary sample from %s. Waiting for the next full discovery.", node.Name)
			return
		}
	}

	thresholds, err := kc.GetKubeletThresholds(ip, node.Name)
	if err != nil {
		glog.Warningf("Failed to get kubelet thresholds for %s, %v.", node.Name, err)
	}

	// TODO Use time stamp attached to the discovered CPUStats/MemoryStats of node and pod from kubelet to be more precise
	currentMilliSec := time.Now().UnixNano() / int64(time.Millisecond)
	m.parseNodeStats(summary.Node, thresholds, currentMilliSec)
	m.parsePodStats(summary.Pods, currentMilliSec)

	glog.V(4).Infof("Finished scrape node %s.", node.Name)
}

func (m *KubeletMonitor) parseNodeCpuFreq(node *api.Node, cpuFrequencyMHz float64) {
	glog.V(4).Infof("node-%s cpuFrequency = %.2fMHz", node.Name, cpuFrequencyMHz)
	cpuFrequencyMetric := metrics.NewEntityStateMetric(metrics.NodeType, util.NodeKeyFunc(node), metrics.CpuFrequency, cpuFrequencyMHz)
	m.metricSink.AddNewMetricEntries(cpuFrequencyMetric)
}

// Parse node stats and put it into sink.
func (m *KubeletMonitor) parseNodeStats(nodeStats stats.NodeStats, thresholds []evictionapi.Threshold, timestamp int64) {
	var cpuUsageCore, memoryWorkingSetBytes, memoryAvailableBytes, rootfsCapacityBytes,
		rootfsUsedMegaBytes, imagefsCapacityBytes, imagefsUsedMegaBytes float64
	// cpu
	if nodeStats.CPU != nil && nodeStats.CPU.UsageNanoCores != nil {
		cpuUsageCore = util.MetricNanoToUnit(float64(*nodeStats.CPU.UsageNanoCores))
	}
	if nodeStats.Memory != nil && nodeStats.Memory.WorkingSetBytes != nil {
		memoryWorkingSetBytes = float64(*nodeStats.Memory.WorkingSetBytes)
	}
	if nodeStats.Memory != nil && nodeStats.Memory.AvailableBytes != nil {
		memoryAvailableBytes = float64(*nodeStats.Memory.AvailableBytes)
	}
	if nodeStats.Fs != nil && nodeStats.Fs.CapacityBytes != nil {
		rootfsCapacityBytes = float64(*nodeStats.Fs.CapacityBytes)
	}
	if nodeStats.Fs != nil && nodeStats.Fs.UsedBytes != nil {
		// OpsMgr server expects the reported size in megabytes
		rootfsUsedMegaBytes = util.Base2BytesToMegabytes(float64(*nodeStats.Fs.UsedBytes))
	}
	if nodeStats.Runtime != nil && nodeStats.Runtime.ImageFs != nil && nodeStats.Runtime.ImageFs.CapacityBytes != nil {
		imagefsCapacityBytes = float64(*nodeStats.Runtime.ImageFs.CapacityBytes)
	}
	if nodeStats.Runtime != nil && nodeStats.Runtime.ImageFs != nil && nodeStats.Runtime.ImageFs.UsedBytes != nil {
		// OpsMgr server expects the reported size in megabytes
		imagefsUsedMegaBytes = util.Base2BytesToMegabytes(float64(*nodeStats.Runtime.ImageFs.UsedBytes))
	}

	key := util.NodeStatsKeyFunc(nodeStats)
	nodeName := nodeStats.NodeName
	memoryWorkingSetKiloBytes := util.Base2BytesToKilobytes(memoryWorkingSetBytes)
	memoryCapacityBytes := memoryAvailableBytes + memoryWorkingSetBytes
	rootfsCapacityMegaBytes := util.Base2BytesToMegabytes(rootfsCapacityBytes)
	imagefsCapacityMegaBytes := util.Base2BytesToMegabytes(imagefsCapacityBytes)
	glog.V(4).Infof("CPU usage of node %s is %.3f core", nodeName, cpuUsageCore)
	glog.V(4).Infof("Memory working set of node %s is %.3f KB", nodeName, memoryWorkingSetKiloBytes)
	glog.V(4).Infof("Memory capacity for node %s is %.3f Bytes", nodeName, memoryCapacityBytes)
	glog.V(4).Infof("Root File System size for node %s is %.3f Megabytes", nodeName, rootfsCapacityMegaBytes)
	glog.V(4).Infof("Root File System used for node %s is %.3f Megabytes", nodeName, rootfsUsedMegaBytes)
	glog.V(4).Infof("Image File System size for node %s is %.3f Megabytes", nodeName, imagefsCapacityMegaBytes)
	glog.V(4).Infof("Image File System used for node %s is %.3f Megabytes", nodeName, imagefsUsedMegaBytes)

	m.genUsedMetrics(metrics.NodeType, key, cpuUsageCore, memoryWorkingSetKiloBytes, timestamp)
	// Collect node fsMetrics only in full discovery not in sampling discovery
	if m.isFullDiscovery {
		imagefsKey := fmt.Sprintf("%s-imagefs", key)
		m.genFSMetrics(metrics.NodeType, key, rootfsCapacityMegaBytes, rootfsUsedMegaBytes)
		m.genFSMetrics(metrics.NodeType, imagefsKey, imagefsCapacityMegaBytes, imagefsUsedMegaBytes)
		m.parseThresholdValues(key, memoryCapacityBytes, rootfsCapacityBytes, imagefsCapacityBytes, thresholds)
	}
}

func (m *KubeletMonitor) parseThresholdValues(key string, memoryCapacity, rootfsCapacity, imagefsCapacity float64, thresholds []evictionapi.Threshold) {
	var memThreshold, rootfsThreshold, imagefsThreshold float64

	for _, threshold := range thresholds {
		switch threshold.Signal {
		case evictionapi.SignalMemoryAvailable:
			memThreshold = GetThresholdPercentile(threshold.Value, memoryCapacity)
		case evictionapi.SignalNodeFsAvailable:
			rootfsThreshold = GetThresholdPercentile(threshold.Value, rootfsCapacity)
		case evictionapi.SignalImageFsAvailable:
			imagefsThreshold = GetThresholdPercentile(threshold.Value, imagefsCapacity)
		default:
		}
	}

	// Ref: https://kubernetes.io/docs/tasks/administer-cluster/out-of-resource/#hard-eviction-thresholds
	// Ref: https://github.com/kubernetes/kubernetes/blob/master/pkg/kubelet/apis/config/v1beta1/defaults_others.go#L22
	// The configured thresholds are value going less then available bytes or percentage.
	if memThreshold <= 0 || memThreshold >= 100 {
		// default 100Mi
		memThreshold = util.Base2MegabytesToBytes(100) * 100 / memoryCapacity
	}
	if rootfsThreshold <= 0 || rootfsThreshold >= 100 {
		// default 10%
		rootfsThreshold = 10
	}
	if imagefsThreshold <= 0 || imagefsThreshold >= 100 {
		// default 15%
		rootfsThreshold = 15
	}

	imagefsKey := fmt.Sprintf("%s-imagefs", key)
	m.metricSink.AddNewMetricEntries(metrics.NewEntityResourceMetric(metrics.NodeType, key, metrics.Memory, metrics.Threshold, memThreshold))
	m.metricSink.AddNewMetricEntries(metrics.NewEntityResourceMetric(metrics.NodeType, key, metrics.VStorage, metrics.Threshold, rootfsThreshold))
	m.metricSink.AddNewMetricEntries(metrics.NewEntityResourceMetric(metrics.NodeType, imagefsKey, metrics.VStorage, metrics.Threshold, imagefsThreshold))
	glog.V(4).Infof("Memory threshold for node %s is %.3f", key, memThreshold)
	glog.V(4).Infof("Rootfs threshold for node %s is %.3f", key, rootfsThreshold)
	glog.V(4).Infof("Imagefs threshold for node %s is %.3f", key, imagefsThreshold)

}

func GetThresholdPercentile(value evictionapi.ThresholdValue, capacity float64) float64 {
	if value.Percentage != 0 {
		// The percentage value parsed in threshold is in decimal points wrt 1
		// eg. 20% is represented as .20
		return float64(value.Percentage) * 100
	}
	return float64(value.Quantity.Value()) * 100 / capacity
}

// Parse pod stats for every pod and put them into sink.
func (m *KubeletMonitor) parsePodStats(podStats []stats.PodStats, timestamp int64) {
	for i := range podStats {
		pod := &(podStats[i])
		cpuUsed, memUsed := m.parseContainerStats(pod, timestamp)
		key := util.PodMetricId(&(pod.PodRef))

		ephemeralFsCapacity, ephemeralFsUsed := float64(0), float64(0)
		if pod.EphemeralStorage != nil {
			if pod.EphemeralStorage.CapacityBytes != nil {
				ephemeralFsCapacity = util.Base2BytesToMegabytes(float64(*pod.EphemeralStorage.CapacityBytes))
			}
			if pod.EphemeralStorage.UsedBytes != nil {
				// OpsMgr server expects the reported size in megabytes
				ephemeralFsUsed = util.Base2BytesToMegabytes(float64(*pod.EphemeralStorage.UsedBytes))
			}
		} else {
			glog.V(4).Infof("Ephemeral fs status is not available for pod %v", key)
		}

		glog.V(4).Infof("Cpu usage of pod %s is %.3f core", key, cpuUsed)
		glog.V(4).Infof("Memory usage of pod %s is %.3f Kb", key, memUsed)
		glog.V(4).Infof("Ephemeral fs capacity for pod %s is %.3f Megabytes", key, ephemeralFsCapacity)
		glog.V(4).Infof("Ephemeral fs used for pod %s is %.3f Megabytes", key, ephemeralFsUsed)

		m.genUsedMetrics(metrics.PodType, key, cpuUsed, memUsed, timestamp)
		// Collect pod numConsumersUsedMetrics and fsMetrics only in full discovery not in sampling discovery
		if m.isFullDiscovery {
			m.genNumConsumersUsedMetrics(metrics.PodType, key)
			m.genFSMetrics(metrics.PodType, key, ephemeralFsCapacity, ephemeralFsUsed)
			if utilfeature.DefaultFeatureGate.Enabled(features.PersistentVolumes) {
				m.parseVolumeStats(pod.VolumeStats, key)
			}
		}
	}
}

func (m *KubeletMonitor) parseVolumeStats(volStats []stats.VolumeStats, podKey string) {
	for i := range volStats {
		volStat := volStats[i]
		capacity, used := float64(0), float64(0)
		if volStat.CapacityBytes != nil {
			capacity = util.Base2BytesToMegabytes(float64(*volStat.CapacityBytes))
		}
		if volStat.UsedBytes != nil {
			used = util.Base2BytesToMegabytes(float64(*volStat.UsedBytes))
		}

		volKey := util.PodVolumeMetricId(podKey, volStat.Name)
		// TODO: Generate used on etype pod and capacity on etype volume once
		// etype volume is in place
		m.genPVMetrics(metrics.PodType, volKey, capacity, used)

		glog.V(4).Infof("Volume Usage of %s mounted by pod %s is %.3f Megabytes", volStat.Name, podKey, used)
		glog.V(4).Infof("Volume Capacity of %s mounted by pod %s is %.3f Megabytes", volStat.Name, podKey, capacity)
	}
}

func (m *KubeletMonitor) parseContainerStats(pod *stats.PodStats, timestamp int64) (float64, float64) {

	totalUsedCPU := float64(0.0)
	totalUsedMem := float64(0.0)

	podMId := util.PodMetricId(&(pod.PodRef))
	containers := pod.Containers

	for i := range containers {
		container := &containers[i]
		if container.CPU == nil || container.CPU.UsageNanoCores == nil {
			continue
		}
		if container.Memory == nil || container.Memory.WorkingSetBytes == nil {
			continue
		}

		cpuUsed := util.MetricNanoToUnit(float64(*container.CPU.UsageNanoCores))
		memUsed := util.Base2BytesToKilobytes(float64(*container.Memory.WorkingSetBytes))

		totalUsedCPU += cpuUsed
		totalUsedMem += memUsed

		//1. container Used
		containerMId := util.ContainerMetricId(podMId, container.Name)
		// Generate used metrics for VCPU and VMemory commodities
		m.genUsedMetrics(metrics.ContainerType, containerMId, cpuUsed, memUsed, timestamp)
		// Generate used metrics for VCPURequest and VMemRequest commodities
		m.genRequestUsedMetrics(metrics.ContainerType, containerMId, cpuUsed, memUsed, timestamp)

		glog.V(4).Infof("container[%s-%s] cpu/memory/cpuRequest/memoryRequest usage:%.3f, %.3f, %.3f, %.3f",
			pod.PodRef.Name, container.Name, cpuUsed, memUsed, cpuUsed, memUsed)

		//2. app Used
		appMId := util.ApplicationMetricId(containerMId)
		m.genUsedMetrics(metrics.ApplicationType, appMId, cpuUsed, memUsed, timestamp)
	}

	return totalUsedCPU, totalUsedMem
}

func (m *KubeletMonitor) genUsedMetrics(etype metrics.DiscoveredEntityType, key string, cpu, memory float64, timestamp int64) {
	// Pass timestamp as parameter instead of generating a new timestamp here to make sure timestamp is same for all
	// corresponding metrics which are scraped from kubelet at the same time
	cpuMetric := metrics.NewEntityResourceMetric(etype, key, metrics.CPU, metrics.Used,
		[]metrics.Point{{
			Value:     cpu,
			Timestamp: timestamp,
		}})
	memMetric := metrics.NewEntityResourceMetric(etype, key, metrics.Memory, metrics.Used,
		[]metrics.Point{{
			Value:     memory,
			Timestamp: timestamp,
		}})
	m.metricSink.AddNewMetricEntries(cpuMetric, memMetric)
}

// genRequestUsedMetrics generates used metrics for VCPURequest and VMemRequest commodity
func (m *KubeletMonitor) genRequestUsedMetrics(etype metrics.DiscoveredEntityType, key string, cpu, memory float64, timestamp int64) {
	// Pass timestamp as parameter instead of generating a new timestamp here to make sure timestamp is same for all
	// corresponding metrics which are scraped from kubelet at the same time
	cpuRequestMetric := metrics.NewEntityResourceMetric(etype, key, metrics.CPURequest, metrics.Used,
		[]metrics.Point{{
			Value:     cpu,
			Timestamp: timestamp,
		}})
	memRequestMetric := metrics.NewEntityResourceMetric(etype, key, metrics.MemoryRequest, metrics.Used,
		[]metrics.Point{{
			Value:     memory,
			Timestamp: timestamp,
		}})
	m.metricSink.AddNewMetricEntries(cpuRequestMetric, memRequestMetric)
}

func (m *KubeletMonitor) genNumConsumersUsedMetrics(etype metrics.DiscoveredEntityType, key string) {
	// Each pod consumes one from numConsumers / Total available is node allocatable pod number
	numConsumersMetric := metrics.NewEntityResourceMetric(etype, key, metrics.NumPods, metrics.Used, float64(1))
	m.metricSink.AddNewMetricEntries(numConsumersMetric)
}

func (m *KubeletMonitor) genFSMetrics(etype metrics.DiscoveredEntityType, key string, capacity, used float64) {
	capacityMetric := metrics.NewEntityResourceMetric(etype, key, metrics.VStorage, metrics.Capacity, capacity)
	usedMetric := metrics.NewEntityResourceMetric(etype, key, metrics.VStorage, metrics.Used, used)
	m.metricSink.AddNewMetricEntries(capacityMetric, usedMetric)
}

func (m *KubeletMonitor) genPVMetrics(etype metrics.DiscoveredEntityType, key string, capacity, used float64) {
	capacityMetric := metrics.NewEntityResourceMetric(etype, key, metrics.StorageAmount, metrics.Capacity, capacity)
	usedMetric := metrics.NewEntityResourceMetric(etype, key, metrics.StorageAmount, metrics.Used, used)
	m.metricSink.AddNewMetricEntries(capacityMetric, usedMetric)
}
