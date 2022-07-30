package master

import (
	"errors"
	"fmt"

	api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
)

// ClusterMonitor is based on Kubenetes's Node, Pod and Container settings;
// it will mainly generate CPU/Memory commodity's Capacity and Request for Node, Pod, and Container;
type ClusterMonitor struct {
	config        *ClusterMonitorConfig
	clusterClient *cluster.ClusterScraper

	//TODO: since this sink is not accessed by multiple goroutines
	// an Add() interface without lock should be provided.
	sink *metrics.EntityMetricSink

	node *api.Node

	nodePodMap map[string][]*api.Pod
	podOwners  map[string]util.OwnerInfo
}

func NewClusterMonitor(config *ClusterMonitorConfig) (*ClusterMonitor, error) {
	return &ClusterMonitor{
		config:        config,
		clusterClient: config.clusterInfoScraper,
		podOwners:     make(map[string]util.OwnerInfo),
	}, nil
}

func (m *ClusterMonitor) GetMonitoringSource() types.MonitoringSource {
	return types.ClusterSource
}

func (m *ClusterMonitor) ReceiveTask(task *task.Task) {
	m.reset()
	m.node = task.Node()
	m.nodePodMap = util.GroupPodsByNode(task.PodList())
}

func (m *ClusterMonitor) Do() (*metrics.EntityMetricSink, error) {
	glog.V(4).Infof("%s has started task.", m.GetMonitoringSource())
	err := m.RetrieveClusterStat()
	if err != nil {
		glog.Errorf("Failed to execute task: %s", err)
		return m.sink, err
	}
	glog.V(4).Infof("%s monitor has finished task.", m.GetMonitoringSource())
	return m.sink, nil
}

// RetrieveClusterStat retrieves resource stats for the received node.
func (m *ClusterMonitor) RetrieveClusterStat() error {
	if m.node == nil {
		return errors.New("invalid node or empty node. Nothing to monitor")
	}
	err := m.findClusterID()
	if err != nil {
		return fmt.Errorf("failed to find cluster ID based on Kubernetes service: %v", err)
	}
	err = m.findNodeStates()
	if err != nil {
		return fmt.Errorf("failed to find node states: %v", err)
	}
	return nil
}

func (m *ClusterMonitor) reset() {
	m.sink = metrics.NewEntityMetricSink()
}

// ----------------------------------------------- Cluster State -------------------------------------------------
// TODO: Getting repeated by cluster worker
//// Get the cluster ID of the Kubernetes cluster.
//// Use Kubernetes service UID as the key for cluster commodity
func (m *ClusterMonitor) findClusterID() error {
	kubernetesSvcID, err := m.config.clusterInfoScraper.GetKubernetesServiceID()
	if err != nil {
		return err
	}
	// TODO use a constant for cluster commodity key.
	clusterInfo := metrics.NewEntityStateMetric(metrics.ClusterType, "", metrics.Cluster, kubernetesSvcID)
	m.sink.AddNewMetricEntries(clusterInfo)
	glog.V(3).Infof("Successfully added cluster info metrics for cluster %v", kubernetesSvcID)
	return nil
}

// ----------------------------------------- Node State --------------------------------------------
func (m *ClusterMonitor) findNodeStates() error {
	key := util.NodeKeyFunc(m.node)
	if key == "" {
		return fmt.Errorf("invalid node")
	}
	// node/pod/container cpu/mem resource capacities
	m.genNodeResourceMetrics(m.node, key)

	// node labels
	labelMetrics := parseNodeLabels(m.node)
	m.sink.AddNewMetricEntries(labelMetrics)
	glog.V(3).Infof("Successfully generated label metrics for node %s", key)
	return nil
	// owner labels - TODO:
}

// Generate resource metrics of a node:
// 	CPU             capacity
// 	Memory          capacity
//	CPURequest      capacity, used
//	MemoryRequest   capacity, used
func (m *ClusterMonitor) genNodeResourceMetrics(node *api.Node, key string) {
	glog.V(3).Infof("Now get resource metrics for node %s", key)

	//1. Capacity of CPU and Memory
	//1.1 Get the total resource of a node
	cpuCapacityMillicore, memoryCapacityKiloBytes := util.GetCpuAndMemoryValues(node.Status.Capacity)
	glog.V(4).Infof("CPU capacity of node %s is %f Core", node.Name, util.MetricMilliToUnit(cpuCapacityMillicore))
	glog.V(4).Infof("Memory capacity of node %s is %f Kb", node.Name, memoryCapacityKiloBytes)
	//1.2 Generate the capacity metric for CPU and Mem
	m.genCapacityMetrics(metrics.NodeType, key, cpuCapacityMillicore, memoryCapacityKiloBytes)

	//2. Capacity of CPURequest and MemoryRequest
	//2.1 Get the allocatable resource of a node
	cpuRequestCapacityMillicore, memoryRequestCapacityKiloBytes := util.GetCpuAndMemoryValues(node.Status.Allocatable)
	glog.V(4).Infof("Allocatable CPU capacity of node %s is %f Core", node.Name, util.MetricMilliToUnit(cpuRequestCapacityMillicore))
	glog.V(4).Infof("Allocatable Memory capacity of node %s is %f Kb", node.Name, memoryRequestCapacityKiloBytes)
	//2.2 Generate the capacity metric for CPURequest and MemRequest
	m.genRequestCapacityMetrics(metrics.NodeType, key, cpuRequestCapacityMillicore, memoryRequestCapacityKiloBytes)

	//3. Generate metrics for hosted Pods and containers
	//The return value of this method is the totalCPURequest and totalMemRequest used on the node
	nodeCPURequestUsed, nodeMemRequestUsed, currentPods := m.genNodePodsMetrics(node, cpuCapacityMillicore, memoryCapacityKiloBytes,
		cpuRequestCapacityMillicore, memoryRequestCapacityKiloBytes)

	//4. Generate the numconsumers (current pod number and actual allocatable pods) metrics for the given node
	allocatablePods := util.GetNumPodsAllocatable(node)
	m.genNumConsumersMetrics(metrics.NodeType, key, currentPods, allocatablePods)
	glog.V(4).Infof("There are %f pods currently on node %s and allocatable pod limit is %f", currentPods, node.Name, allocatablePods)

	//5. Generate the used metric for CPURequest and MemRequest for the node
	m.genRequestUsedMetrics(metrics.NodeType, key, nodeCPURequestUsed, nodeMemRequestUsed)
	glog.V(4).Infof("CPURequest used of node %s is %f Millicore", node.Name, nodeCPURequestUsed)
	glog.V(4).Infof("MemoryRequest used of node %s is %f Kb", node.Name, nodeMemRequestUsed)
	glog.V(3).Infof("Successfully generated resource metrics for node %s", key)
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
		return metrics.NewEntityStateMetric(metrics.NodeType, util.NodeKeyFunc(node), metrics.Access, labels)
	}
	return metrics.EntityStateMetric{}
}

// ----------------------------------------------- Pod State -------------------------------------------------
// generate all the metrics for the hosted Pods of this node.
// Resource metrics such as capacity and usage
func (m *ClusterMonitor) genNodePodsMetrics(node *api.Node, cpuCapacityMillicore, memCapacity, cpuRequestCapacityMillicore,
	memoryRequestCapacity float64) (nodeCPURequestUsedMillicore float64, nodeMemoryRequestUsedKiloBytes float64, numPods float64) {
	// Get the pod list for the node
	podList, exist := m.nodePodMap[node.Name]
	if !exist || len(podList) < 1 {
		glog.V(3).Infof("Node[%s] has no pod", node.Name)
		return
	}

	// Iterate over each pod
	for _, pod := range podList {
		key := util.PodKeyFunc(pod)
		// Pod owners
		ownerInfo, _, _, err := m.clusterClient.GetPodControllerInfo(pod, true)
		if err == nil && !util.IsOwnerInfoEmpty(ownerInfo) {
			m.podOwners[key] = ownerInfo
		}

		// Pod capacity metrics and Container resources metric
		podCPURequest, podMemoryRequest := m.genPodMetrics(pod, cpuCapacityMillicore, memCapacity, cpuRequestCapacityMillicore, memoryRequestCapacity)
		nodeCPURequestUsedMillicore += podCPURequest
		nodeMemoryRequestUsedKiloBytes += podMemoryRequest
	}

	numPods = float64(len(podList))
	glog.V(3).Infof("Successfully generated pod metrics for node %v.", node.Name)
	return
}

// genPodMetrics: based on hosting Node's cpuCapacity, memCapacity, cpuAllocatable and memAllocatable
// (1) generate Container CPU/Memory capacity, CPURequest/MemoryRequest capacity and CPU/memory limit and request quota used
// (resource quota used is the same as corresponding resource capacity)
// (2) generate Pod CPU/Memory capacity and CPURequest/MemoryRequest capacity
// (3) Pod CPURequest/MemoryRequest usage is the sum of containers CPURequest/MemoryRequest capacity
func (m *ClusterMonitor) genPodMetrics(pod *api.Pod, nodeCPUCapacityMillicore, nodeMemCapacity, nodeCPUAllocatableMillicore,
	nodeMemAllocatable float64) (float64, float64) {
	key := util.PodKeyFunc(pod)
	glog.V(4).Infof("begin to generate pod[%s]'s CPU/Mem Capacity.", key)

	//1. pod.capacity == node.Capacity
	cpuCapacityMillicore := nodeCPUCapacityMillicore
	memCapacity := nodeMemCapacity
	podMId := util.PodMetricIdAPI(pod)
	m.genCapacityMetrics(metrics.PodType, podMId, cpuCapacityMillicore, memCapacity)

	//2. Requests
	//2.1 Get the totalCPURequest and totalMemRequest from all containers in the pod
	podCPURequest, podMemRequest := m.genContainerMetrics(pod, cpuCapacityMillicore, memCapacity)
	//2.2 Generate capacity metric for CPURequest and MemRequest. Pod requests capacity is node Allocatable
	m.genRequestCapacityMetrics(metrics.PodType, podMId, nodeCPUAllocatableMillicore, nodeMemAllocatable)
	//2.3 Generate used metric for CPURequest and MemRequest
	m.genRequestUsedMetrics(metrics.PodType, podMId, podCPURequest, podMemRequest)

	//3. Owner
	podOwner, exists := m.podOwners[key]
	if exists {
		m.genOwnerMetrics(metrics.PodType, key, podOwner.Kind, podOwner.Name, podOwner.Uid)
	}

	return podCPURequest, podMemRequest
}

// Container.Capacity = container.Limit if limit is set, otherwise is Pod.Capacity
// Application won't sell CPU/Memory, so no need to generate application CPU/Memory Capacity for application
func (m *ClusterMonitor) genContainerMetrics(pod *api.Pod, podCPUMillicore, podMem float64) (float64, float64) {

	totalCPURequest := float64(0.0)
	totalMemRequest := float64(0.0)
	podMId := util.PodMetricIdAPI(pod)
	podKey := util.PodKeyFunc(pod)

	for i := range pod.Spec.Containers {
		container := &(pod.Spec.Containers[i])
		containerMId := util.ContainerMetricId(podMId, container.Name)

		//1. CPU, Memory, CPULimitQuota and MemoryLimitQuota capacity
		limits := container.Resources.Limits
		cpuLimit := limits.Cpu().MilliValue()
		memLimit := limits.Memory().Value()
		cpuCapacityMillicore := podCPUMillicore
		memCapacity := podMem

		if cpuLimit > 1 {
			cpuCapacityMillicore = float64(cpuLimit)
		}

		if memLimit > 1 {
			memCapacity = util.Base2BytesToKilobytes(float64(memLimit))
		}
		m.genCapacityMetrics(metrics.ContainerType, containerMId, cpuCapacityMillicore, memCapacity)
		// Generate resource limit quota metrics with used value as CPU/memory resource capacity
		m.genLimitQuotaUsedMetrics(metrics.ContainerType, containerMId, cpuCapacityMillicore, memCapacity)

		//2. CPURequest, MemoryRequest, CPURequestQuota and MemoryRequestQuota capacity
		requests := container.Resources.Requests
		cpuRequest := float64(requests.Cpu().MilliValue())
		memRequest := util.Base2BytesToKilobytes(float64(requests.Memory().Value()))
		m.genRequestCapacityMetrics(metrics.ContainerType, containerMId, cpuRequest, memRequest)
		// Generate resource request quota metrics with used value as CPU/memory resource request capacity
		m.genRequestQuotaUsedMetrics(metrics.ContainerType, containerMId, cpuRequest, memRequest)

		totalCPURequest += cpuRequest
		totalMemRequest += memRequest

		//3. Owner
		podOwner, exists := m.podOwners[podKey]
		if exists {
			m.genContainerSidecarMetric(containerMId, IsInjectedSidecar(container.Name, podOwner.Containers))
			m.genOwnerMetrics(metrics.ContainerType, containerMId, podOwner.Kind, podOwner.Name, podOwner.Uid)
		}
	}

	return totalCPURequest, totalMemRequest
}

func IsInjectedSidecar(name string, containers sets.String) bool {
	return containers != nil && !containers.Has(name)
}

func (m *ClusterMonitor) genContainerSidecarMetric(key string, IsInjectedSidecar bool) {
	sidecarMetric := metrics.NewEntityStateMetric(metrics.ContainerType, key, metrics.IsInjectedSidecar, IsInjectedSidecar)
	m.sink.AddNewMetricEntries(sidecarMetric)
}

func (m *ClusterMonitor) genOwnerMetrics(etype metrics.DiscoveredEntityType, key, kind, parentName, uid string) {
	if parentName != "" && kind != "" && uid != "" {
		ownerMetric := metrics.NewEntityStateMetric(etype, key, metrics.Owner, parentName)
		ownerTypeMetric := metrics.NewEntityStateMetric(etype, key, metrics.OwnerType, kind)
		ownerUIDMetric := metrics.NewEntityStateMetric(etype, key, metrics.OwnerUID, uid)
		m.sink.AddNewMetricEntries(ownerMetric)
		m.sink.AddNewMetricEntries(ownerTypeMetric)
		m.sink.AddNewMetricEntries(ownerUIDMetric)
	}
}

// genRequestUsedMetrics generates used metrics for VCPURequest and VMemRequest commodity
func (m *ClusterMonitor) genRequestUsedMetrics(etype metrics.DiscoveredEntityType, key string, cpu, memory float64) {
	cpuMetric := metrics.NewEntityResourceMetric(etype, key, metrics.CPURequest, metrics.Used, cpu)
	memMetric := metrics.NewEntityResourceMetric(etype, key, metrics.MemoryRequest, metrics.Used, memory)
	m.sink.AddNewMetricEntries(cpuMetric, memMetric)
}

// genRequestCapacityMetrics generates capacity metrics for VCPURequest and VMemRequest commodity
func (m *ClusterMonitor) genRequestCapacityMetrics(etype metrics.DiscoveredEntityType, key string, cpu, memory float64) {
	cpuMetric := metrics.NewEntityResourceMetric(etype, key, metrics.CPURequest, metrics.Capacity, cpu)
	memMetric := metrics.NewEntityResourceMetric(etype, key, metrics.MemoryRequest, metrics.Capacity, memory)
	m.sink.AddNewMetricEntries(cpuMetric, memMetric)
}

// genCapacityMetrics generates capacity metrics for VCPU and VMemory commodity
func (m *ClusterMonitor) genCapacityMetrics(etype metrics.DiscoveredEntityType, key string, cpu, memory float64) {
	cpuMetric := metrics.NewEntityResourceMetric(etype, key, metrics.CPU, metrics.Capacity, cpu)
	memMetric := metrics.NewEntityResourceMetric(etype, key, metrics.Memory, metrics.Capacity, memory)
	m.sink.AddNewMetricEntries(cpuMetric, memMetric)
}

// genLimitQuotaUsedMetrics generates used metrics for CPULimitQuota and MemoryLimitQuota resources
func (m *ClusterMonitor) genLimitQuotaUsedMetrics(etype metrics.DiscoveredEntityType, key string, cpu, memory float64) {
	cpuMetric := metrics.NewEntityResourceMetric(etype, key, metrics.CPULimitQuota, metrics.Used, cpu)
	memMetric := metrics.NewEntityResourceMetric(etype, key, metrics.MemoryLimitQuota, metrics.Used, memory)
	m.sink.AddNewMetricEntries(cpuMetric, memMetric)
}

// genRequestQuotaUsedMetrics generates used metrics for CPURequestQuota and MemoryRequestQuota resources
func (m *ClusterMonitor) genRequestQuotaUsedMetrics(etype metrics.DiscoveredEntityType, key string, cpu, memory float64) {
	cpuMetric := metrics.NewEntityResourceMetric(etype, key, metrics.CPURequestQuota, metrics.Used, cpu)
	memMetric := metrics.NewEntityResourceMetric(etype, key, metrics.MemoryRequestQuota, metrics.Used, memory)
	m.sink.AddNewMetricEntries(cpuMetric, memMetric)
}

// genNumConsumersMetrics generates NumConsumers commodity equivalant of numpods used and allocatable for a k8s node (maps to CommodityDTO_NUMBER_CONSUMERS)
func (m *ClusterMonitor) genNumConsumersMetrics(etype metrics.DiscoveredEntityType, key string, used, allocatable float64) {
	podsCapacityMetric := metrics.NewEntityResourceMetric(etype, key, metrics.NumPods, metrics.Capacity, allocatable)
	podsUsedMetric := metrics.NewEntityResourceMetric(etype, key, metrics.NumPods, metrics.Used, used)
	m.sink.AddNewMetricEntries(podsCapacityMetric, podsUsedMetric)
}
