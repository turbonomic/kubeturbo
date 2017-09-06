package master

import (
	"errors"
	"fmt"

	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	"github.com/golang/glog"
)

// This monitor is based on Kubenetes's Node, Pod and Container settings;
// it will mainly generate CPU/Memory commodity's Capacity and Provision for Node, Pod, and Container;
type ClusterMonitor struct {
	config *ClusterMonitorConfig

	//TODO: since this sink is not accessed by multiple goroutines
	// an Add() interface without lock should be provided.
	sink *metrics.EntityMetricSink

	nodeList []*api.Node

	nodePodMap map[string][]*api.Pod

	stopCh chan struct{}
}

func NewClusterMonitor(config *ClusterMonitorConfig) (*ClusterMonitor, error) {

	return &ClusterMonitor{
		config: config,
		stopCh: make(chan struct{}, 1),
	}, nil
}

func (m *ClusterMonitor) GetMonitoringSource() types.MonitoringSource {
	return types.ClusterSource
}

func (m *ClusterMonitor) ReceiveTask(task *task.Task) {
	m.reset()

	m.nodeList = task.NodeList()
	m.nodePodMap = util.GroupPodsByNode(task.PodList())
}

func (m *ClusterMonitor) Stop() {
	m.stopCh <- struct{}{}
}

func (m *ClusterMonitor) Do() *metrics.EntityMetricSink {
	glog.V(4).Infof("%s has started task.", m.GetMonitoringSource())
	err := m.RetrieveClusterStat()
	if err != nil {
		glog.Errorf("Failed to execute task: %s", err)
	}
	glog.V(4).Infof("%s monitor has finished task.", m.GetMonitoringSource())
	return m.sink
}

// Start to retrieve resource stats for the received list of nodes.
func (m *ClusterMonitor) RetrieveClusterStat() error {
	defer close(m.stopCh)

	if m.nodeList == nil {
		return errors.New("Invalid nodeList or empty nodeList. Nothing to monitor.")
	}
	select {
	case <-m.stopCh:
		return nil
	default:
		err := m.findClusterID()
		if err != nil {
			return fmt.Errorf("Failed to find cluster ID based on Kubernetes service: %v", err)
		}
		select {
		case <-m.stopCh:
			return nil
		default:
			m.findNodeStates()
		}

		return nil
	}
}

func (m *ClusterMonitor) reset() {
	m.sink = metrics.NewEntityMetricSink()
	m.stopCh = make(chan struct{}, 1)
}

// ----------------------------------------------- Cluster State -------------------------------------------------
// Get the cluster ID of the Kubernetes cluster.
// Use Kubernetes service UID as the key for cluster commodity
func (m *ClusterMonitor) findClusterID() error {
	kubernetesSvcID, err := m.config.clusterInfoScraper.GetKubernetesServiceID()
	if err != nil {
		return err
	}
	// TODO use a constant for cluster commodity key.
	clusterInfo := metrics.NewEntityStateMetric(task.ClusterType, "", metrics.Cluster, kubernetesSvcID)
	m.sink.AddNewMetricEntries(clusterInfo)
	return nil
}

// ----------------------------------------------- Node State -------------------------------------------------
func (m *ClusterMonitor) findNodeStates() {
	for _, node := range m.nodeList {
		key := util.NodeKeyFunc(node)
		if key == "" {
			glog.Warning("Invalid node")
			continue
		}

		m.genNodeResourceMetrics(node)

		labelMetrics := parseNodeLabels(node)
		m.sink.AddNewMetricEntries(labelMetrics)
	}
}

// Generate resource metrics of a node:
// 	CPU 			capacity
// 	memory 			capacity
//	CPUProvisioned		capacity, used
//	memoryProvisioned	capacity, used
func (m *ClusterMonitor) genNodeResourceMetrics(node *api.Node) error {
	key := util.NodeKeyFunc(node)
	glog.V(3).Infof("Now get resouce metrics for node %s", key)

	//1. Capacity of cpu and memory
	cpuCapacityCore, memoryCapacityKiloBytes := util.GetCpuAndMemoryValues(node.Status.Capacity)
	glog.V(4).Infof("Cpu capacity of node %s is %f core", node.Name, cpuCapacityCore)
	glog.V(4).Infof("Memory capacity of node %s is %f Kb", node.Name, memoryCapacityKiloBytes)
	m.genCapacityMetrics(task.NodeType, key, cpuCapacityCore, memoryCapacityKiloBytes)

	//2. Provision Capacity of cpu and memory
	// The provisioned resources have the same capacities.
	cpuProvisionedCapacity := cpuCapacityCore
	memoryProvisionedCapacity := memoryCapacityKiloBytes
	m.genProvisionCapacityMetrics(task.NodeType, key, cpuProvisionedCapacity, memoryProvisionedCapacity)

	//3. Generate metrics for the hosted Pods(and containers)
	m.genNodePodsMetrics(node, cpuCapacityCore, memoryCapacityKiloBytes)

	//4. Provision Used of cpu and memory
	// Provisioned resource used is the sum of all the requested resource of all the pods running in the node.
	// TODO: get these two values based on previous genNodePodsMetrics()
	cpuProvisionedUsed, memProvisionedUsed, err := util.GetNodeResourceRequestConsumption(m.nodePodMap[node.Name])
	if err != nil {
		return fmt.Errorf("failed to get provision used metrics: %v", err)
	}
	m.genProvisionUsedMetrics(task.NodeType, key, cpuProvisionedUsed, memProvisionedUsed)
	glog.V(4).Infof("Cpu provisioned used of node %s is %f core", node.Name, cpuProvisionedUsed)
	glog.V(4).Infof("Memory provisioned used of node %s is %f Kb", node.Name, memProvisionedUsed)
	return nil
}

// Parse the labels of a node and create one EntityStateMetric
func parseNodeLabels(node *api.Node) metrics.EntityStateMetric {
	labelsMap := node.ObjectMeta.Labels
	if len(labelsMap) > 0 {
		var labels []string
		for key, value := range labelsMap {
			l := key + "=" + value
			glog.V(4).Infof("label for this Node is : %s", l)

			labels = append(labels, l)
		}
		return metrics.NewEntityStateMetric(task.NodeType, util.NodeKeyFunc(node), metrics.Access, labels)
	} else {
		return metrics.EntityStateMetric{}
	}
}

// ----------------------------------------------- Pod State -------------------------------------------------
// generate all the metrics for the hosted Pods of this node
func (m *ClusterMonitor) genNodePodsMetrics(node *api.Node, cpuCapacity, memCapacity float64) error {
	podList, exist := m.nodePodMap[node.Name]
	if !exist || len(podList) < 1 {
		glog.V(3).Infof("Node[%s] has no pod", node.Name)
		return nil
	}

	for _, pod := range podList {
		m.genPodMetrics(pod, cpuCapacity, memCapacity)
	}

	return nil
}

// genPodMetrics: based on hosting Node's cpuCapacity and memCapacity
// (1) generate Pod.Capacity and Pod.Reservation
// (2) generate Container.Capacity and Container.Reservation
//
// Note: Pod.Capacity = node.Capacity; Pod.Reservation = sum.container.reservation
func (m *ClusterMonitor) genPodMetrics(pod *api.Pod, nodeCPUCapacity, nodeMemCapacity float64) {
	key := util.PodKeyFunc(pod)
	glog.V(3).Infof("begin to generate pod[%s]'s CPU/Mem Capacity.", key)

	//1. pod.capacity == node.Capacity
	cpuCapacity := nodeCPUCapacity
	memCapacity := nodeMemCapacity
	m.genCapacityMetrics(task.PodType, key, cpuCapacity, memCapacity)

	//2. Reservation
	//2.1 Container Capacity and Reservation
	cpuRequest, memRequest := m.genContainerMetrics(pod, cpuCapacity, memCapacity)
	m.genReserveMetrics(task.PodType, key, cpuRequest, memRequest)
	return
}

// Container.Capacity = container.Limit if limit is set, otherwise is Pod.Capacity
// Application won't sell CPU/Memory, so no need to application CPU/Memory Capacity for application
func (m *ClusterMonitor) genContainerMetrics(pod *api.Pod, podCPU, podMem float64) (float64, float64) {

	totalCPU := float64(0.0)
	totalMem := float64(0.0)
	podId := string(pod.UID)

	for i := range pod.Spec.Containers {
		container := &(pod.Spec.Containers[i])
		key := util.ContainerIdFunc(podId, i)

		//1. capacity
		limits := container.Resources.Limits
		cpuLimit := limits.Cpu().MilliValue()
		memLimit := limits.Memory().Value()
		cpuCapacity := podCPU
		memCapacity := podMem

		if cpuLimit > 1 {
			cpuCapacity = float64(cpuLimit) / util.MilliToUnit
		}

		if memLimit > 1 {
			memCapacity = float64(memLimit) / util.KilobytesToBytes
		}
		m.genCapacityMetrics(task.ContainerType, key, cpuCapacity, memCapacity)

		//2. reservation
		requests := container.Resources.Requests
		cpuRequest := float64(requests.Cpu().MilliValue()) / util.MilliToUnit
		memRequest := float64(requests.Memory().Value()) / util.KilobytesToBytes
		m.genReserveMetrics(task.ContainerType, key, cpuRequest, memRequest)

		totalCPU += cpuRequest
		totalMem += memRequest
	}

	return totalCPU, totalMem
}

func (m *ClusterMonitor) genProvisionUsedMetrics(etype task.DiscoveredEntityType, key string, cpu, memory float64) {
	cpuMetric := metrics.NewEntityResourceMetric(etype, key, metrics.CPUProvisioned, metrics.Used, cpu)
	memMetric := metrics.NewEntityResourceMetric(etype, key, metrics.MemoryProvisioned, metrics.Used, memory)
	m.sink.AddNewMetricEntries(cpuMetric, memMetric)
}

func (m *ClusterMonitor) genProvisionCapacityMetrics(etype task.DiscoveredEntityType, key string, cpu, memory float64) {
	cpuMetric := metrics.NewEntityResourceMetric(etype, key, metrics.CPUProvisioned, metrics.Capacity, cpu)
	memMetric := metrics.NewEntityResourceMetric(etype, key, metrics.MemoryProvisioned, metrics.Capacity, memory)
	m.sink.AddNewMetricEntries(cpuMetric, memMetric)
}

func (m *ClusterMonitor) genCapacityMetrics(etype task.DiscoveredEntityType, key string, cpu, memory float64) {
	cpuMetric := metrics.NewEntityResourceMetric(etype, key, metrics.CPU, metrics.Capacity, cpu)
	memMetric := metrics.NewEntityResourceMetric(etype, key, metrics.Memory, metrics.Capacity, memory)
	m.sink.AddNewMetricEntries(cpuMetric, memMetric)
}

func (m *ClusterMonitor) genReserveMetrics(etype task.DiscoveredEntityType, key string, cpu, memory float64) {
	cpuMetric := metrics.NewEntityResourceMetric(etype, key, metrics.CPU, metrics.Reservation, cpu)
	memMetric := metrics.NewEntityResourceMetric(etype, key, metrics.Memory, metrics.Reservation, memory)
	m.sink.AddNewMetricEntries(cpuMetric, memMetric)
}
