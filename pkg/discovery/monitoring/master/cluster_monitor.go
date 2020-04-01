package master

import (
	"errors"
	"fmt"

	"k8s.io/client-go/dynamic"

	api "k8s.io/api/core/v1"

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

	nodeList []*api.Node

	nodePodMap map[string][]*api.Pod
	podOwners  map[string]*PodOwner

	stopCh chan struct{}
}

type PodOwner struct {
	kind string
	name string
	uid  string
}

func NewClusterMonitor(config *ClusterMonitorConfig) (*ClusterMonitor, error) {

	return &ClusterMonitor{
		config:        config,
		clusterClient: config.clusterInfoScraper,
		stopCh:        make(chan struct{}, 1),
		podOwners:     make(map[string]*PodOwner),
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

// RetrieveClusterStat retrieves resource stats for the received list of nodes.
func (m *ClusterMonitor) RetrieveClusterStat() error {
	defer close(m.stopCh)

	if m.nodeList == nil {
		return errors.New("Invalid nodeList or empty nodeList. Nothing to monitor")
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
	return nil
}

// ----------------------------------------- Node State --------------------------------------------
func (m *ClusterMonitor) findNodeStates() {
	for _, node := range m.nodeList {
		key := util.NodeKeyFunc(node)
		if key == "" {
			glog.Warning("Invalid node")
			continue
		}
		// node/pod/container cpu/mem resource capacities
		m.genNodeResourceMetrics(node, key)

		// node labels
		labelMetrics := parseNodeLabels(node)
		m.sink.AddNewMetricEntries(labelMetrics)

		// owner labels - TODO:

	}
}

// Generate resource metrics of a node:
// 	CPU             capacity
// 	Memory          capacity
//	CPURequest      capacity, used
//	MemoryRequest   capacity, used
func (m *ClusterMonitor) genNodeResourceMetrics(node *api.Node, key string) {
	glog.V(4).Infof("Now get resouce metrics for node %s", key)

	//1. Capacity of CPU and Memory
	//1.1 Get the total resource of a node
	cpuCapacityCore, memoryCapacityKiloBytes := util.GetCpuAndMemoryValues(node.Status.Capacity)
	glog.V(4).Infof("CPU capacity of node %s is %f core", node.Name, cpuCapacityCore)
	glog.V(4).Infof("Memory capacity of node %s is %f Kb", node.Name, memoryCapacityKiloBytes)
	//1.2 Generate the capacity metric for CPU and Mem
	m.genCapacityMetrics(metrics.NodeType, key, cpuCapacityCore, memoryCapacityKiloBytes)

	//2. Capacity of CPURequest and MemoryRequest
	//2.1 Get the allocatable resource of a node
	cpuRequestCapacityCore, memoryRequestCapacityKiloBytes := util.GetCpuAndMemoryValues(node.Status.Allocatable)
	glog.V(4).Infof("Allocatable CPU capacity of node %s is %f core", node.Name, cpuCapacityCore)
	glog.V(4).Infof("Allocatable Memory capacity of node %s is %f Kb", node.Name, memoryCapacityKiloBytes)
	//2.2 Generate the capacity metric for CPURequest and MemRequest
	m.genRequestCapacityMetrics(metrics.NodeType, key, cpuRequestCapacityCore, memoryRequestCapacityKiloBytes)

	//3. Generate metrics for hosted Pods and containers
	//The return value of this method is the totalCPURequest and totalMemRequest used on the node
	nodeCPURequestUsed, nodeMemRequestUsed, currentPods := m.genNodePodsMetrics(node, cpuCapacityCore, memoryCapacityKiloBytes)

	//4. Generate the numconsumers (current pod number and actual allocatable pods) metrics for the given node
	allocatablePods := util.GetNumPodsAllocatable(node)
	m.genNumConsumersMetrics(metrics.NodeType, key, currentPods, allocatablePods)
	glog.V(4).Infof("There are %f pods currently on node %s and allocatable pod limit is %f", currentPods, node.Name, allocatablePods)

	//5. Generate the used metric for CPURequest and MemRequest for the node
	m.genRequestUsedMetrics(metrics.NodeType, key, nodeCPURequestUsed, nodeMemRequestUsed)
	glog.V(4).Infof("CPURequest used of node %s is %f core", node.Name, nodeCPURequestUsed)
	glog.V(4).Infof("MemoryRequest used of node %s is %f Kb", node.Name, nodeMemRequestUsed)
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
func (m *ClusterMonitor) genNodePodsMetrics(node *api.Node, cpuCapacity, memCapacity float64) (nodeCPURequestUsedCore float64, nodeMemoryRequestUsedKiloBytes float64, numPods float64) {
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
		podOwner, err := m.getPodOwner(pod, m.clusterClient.DynamicClient)
		if err == nil {
			m.podOwners[key] = podOwner
		}

		// Pod capacity metrics and Container resources metric
		podCPURequest, podMemoryRequest := m.genPodMetrics(pod, cpuCapacity, memCapacity)
		nodeCPURequestUsedCore += podCPURequest
		nodeMemoryRequestUsedKiloBytes += podMemoryRequest
	}

	numPods = float64(len(podList))
	return
}

func (m *ClusterMonitor) getPodOwner(pod *api.Pod, dynClient dynamic.Interface) (*PodOwner, error) {
	key := util.PodKeyFunc(pod)
	glog.V(4).Infof("begin to generate pod[%s]'s Owner metric.", key)

	kind, parentName, uid, err := util.GetPodGrandInfo(dynClient, pod)
	if err != nil {
		return nil, fmt.Errorf("Error getting pod owner: %v", err)
	}

	if parentName == "" || kind == "" || uid == "" {
		return nil, fmt.Errorf("Invalid pod owner %s::%s::%s", kind, parentName, uid)
	}
	return &PodOwner{kind: kind, name: parentName, uid: uid}, nil
}

// genPodMetrics: based on hosting Node's cpuCapacity and memCapacity
// (1) generate Pod.Capacity and Pod.Reservation
// (2) generate Container.Capacity and Container.Reservation
//
// Note: Pod.Capacity = node.Capacity; Pod.Reservation = sum.container.reservation
func (m *ClusterMonitor) genPodMetrics(pod *api.Pod, nodeCPUCapacity, nodeMemCapacity float64) (float64, float64) {
	key := util.PodKeyFunc(pod)
	glog.V(4).Infof("begin to generate pod[%s]'s CPU/Mem Capacity.", key)

	//1. pod.capacity == node.Capacity
	cpuCapacity := nodeCPUCapacity
	memCapacity := nodeMemCapacity
	podMId := util.PodMetricIdAPI(pod)
	m.genCapacityMetrics(metrics.PodType, podMId, cpuCapacity, memCapacity)

	//2. Reservation
	//2.1 Get the totalCPURequest and totalMemRequest from all containers in the pod
	podCPURequest, podMemRequest := m.genContainerMetrics(pod, cpuCapacity, memCapacity)
	//2.2 Generate reservation metric for CPU and Mem
	m.genReservationMetrics(metrics.PodType, podMId, podCPURequest, podMemRequest)
	//2.3 Generate used metric for CPURequest and MemRequest
	m.genRequestUsedMetrics(metrics.PodType, podMId, podCPURequest, podMemRequest)

	//3. Owner
	podOwner, exists := m.podOwners[key]
	if exists && podOwner != nil {
		m.genOwnerMetrics(metrics.PodType, key, podOwner.kind, podOwner.name, podOwner.uid)
	}

	return podCPURequest, podMemRequest
}

// Container.Capacity = container.Limit if limit is set, otherwise is Pod.Capacity
// Application won't sell CPU/Memory, so no need to generate application CPU/Memory Capacity for application
func (m *ClusterMonitor) genContainerMetrics(pod *api.Pod, podCPU, podMem float64) (float64, float64) {

	totalCPURequest := float64(0.0)
	totalMemRequest := float64(0.0)
	podMId := util.PodMetricIdAPI(pod)
	podKey := util.PodKeyFunc(pod)

	for i := range pod.Spec.Containers {
		container := &(pod.Spec.Containers[i])
		containerMId := util.ContainerMetricId(podMId, container.Name)

		//1. capacity
		limits := container.Resources.Limits
		cpuLimit := limits.Cpu().MilliValue()
		memLimit := limits.Memory().Value()
		cpuCapacity := podCPU
		memCapacity := podMem

		if cpuLimit > 1 {
			cpuCapacity = util.MetricMilliToUnit(float64(cpuLimit))
		}

		if memLimit > 1 {
			memCapacity = util.Base2BytesToKilobytes(float64(memLimit))
		}
		m.genCapacityMetrics(metrics.ContainerType, containerMId, cpuCapacity, memCapacity)

		//2. reservation
		requests := container.Resources.Requests
		cpuRequest := util.MetricMilliToUnit(float64(requests.Cpu().MilliValue()))
		memRequest := util.Base2BytesToKilobytes(float64(requests.Memory().Value()))
		m.genReservationMetrics(metrics.ContainerType, containerMId, cpuRequest, memRequest)

		totalCPURequest += cpuRequest
		totalMemRequest += memRequest

		//3. Owner
		podOwner, exists := m.podOwners[podKey]
		if exists && podOwner != nil {
			m.genOwnerMetrics(metrics.ContainerType, containerMId, podOwner.kind, podOwner.name, podOwner.uid)
		}
	}

	return totalCPURequest, totalMemRequest
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

// genReservationMetrics generates reservation (equivalent of request) metrics for VCPU and VMemory commodity
func (m *ClusterMonitor) genReservationMetrics(etype metrics.DiscoveredEntityType, key string, cpu, memory float64) {
	cpuMetric := metrics.NewEntityResourceMetric(etype, key, metrics.CPU, metrics.Reservation, cpu)
	memMetric := metrics.NewEntityResourceMetric(etype, key, metrics.Memory, metrics.Reservation, memory)
	m.sink.AddNewMetricEntries(cpuMetric, memMetric)
}

// genNumConsumersMetrics generates NumConsumers commodity equivalant of numpods used and allocatable for a k8s node (maps to CommodityDTO_NUMBER_CONSUMERS)
func (m *ClusterMonitor) genNumConsumersMetrics(etype metrics.DiscoveredEntityType, key string, used, allocatable float64) {
	podsCapacityMetric := metrics.NewEntityResourceMetric(etype, key, metrics.NumPods, metrics.Capacity, allocatable)
	podsUsedMetric := metrics.NewEntityResourceMetric(etype, key, metrics.NumPods, metrics.Used, used)
	m.sink.AddNewMetricEntries(podsCapacityMetric, podsUsedMetric)
}
