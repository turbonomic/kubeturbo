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

type ClusterMonitor struct {
	config *ClusterMonitorConfig

	sink *metrics.EntityMetricSink

	nodeList []*api.Node

	nodePodMap map[string][]*api.Pod

	nodeResourceCapacities map[string]*nodeInfo
}

func NewClusterMonitor(config *ClusterMonitorConfig) (*ClusterMonitor, error) {

	return &ClusterMonitor{
		config: config,
	}, nil
}

func (m *ClusterMonitor) GetMonitoringSource() types.MonitoringSource {
	return types.ClusterSource
}

func (m *ClusterMonitor) ReceiveTask(task *task.Task) {
	m.nodeList = task.NodeList()
	m.nodePodMap = util.GroupPodsByNode(task.PodList())
}

func (m *ClusterMonitor) Do() *metrics.EntityMetricSink {
	glog.V(4).Infof("%s has started task.", m.GetMonitoringSource())
	m.reset()
	m.RetrieveClusterStat()
	glog.V(4).Infof("%s monitor has finished task.", m.GetMonitoringSource())
	return m.sink
}

// Start to retrieve resource stats for the received list of nodes.
func (m *ClusterMonitor) RetrieveClusterStat() error {
	if m.nodeList == nil {
		return errors.New("Invalid nodeList or empty nodeList. Nothing to monitor.")
	}
	err := m.findClusterID()
	if err != nil {
		return fmt.Errorf("Failed to find cluster ID based on Kubernetes service: %v", err)
	}

	m.findNodeStates()
	m.findPodStates()

	return nil
}

func (m *ClusterMonitor) reset() {
	m.sink = metrics.NewEntityMetricSink()
	m.nodeResourceCapacities = make(map[string]*nodeInfo)
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
type nodeInfo struct {
	cpuCapacity    float64
	memoryCapacity float64
}

func (m *ClusterMonitor) findNodeStates() {
	for _, node := range m.nodeList {
		key := util.NodeKeyFunc(node)
		if key == "" {
			glog.Warning("Invalid node")
			continue
		}

		nodeResourceMetrics, err := m.getNodeResourceMetrics(node)
		if err != nil {
			glog.Errorf("Failed to get resource metrics of node %s: %s", key, err)
			continue
		}
		// TODO add metric entry directly inside getNodeResourceMetrics
		for _, nrm := range nodeResourceMetrics {
			m.sink.AddNewMetricEntries(nrm)
		}

		labelMetrics := parseNodeLabels(node)
		m.sink.AddNewMetricEntries(labelMetrics)
	}
}

// Get resource metrics of a single node:
// 	CPU 			capacity
// 	memory 			capacity
//	CPUProvisioned		capacity, used
//	memoryProvisioned	capacity, used
func (m *ClusterMonitor) getNodeResourceMetrics(node *api.Node) ([]metrics.EntityResourceMetric, error) {
	key := util.NodeKeyFunc(node)
	glog.V(3).Infof("Now get resouce metrics for node %s", key)

	var nodeResourceMetrics []metrics.EntityResourceMetric

	// cpu and memory
	cpuCapacityCore, memoryCapacityKiloBytes := util.GetCpuAndMemoryValues(node.Status.Capacity)
	m.nodeResourceCapacities[node.Name] = &nodeInfo{
		cpuCapacity:    cpuCapacityCore,
		memoryCapacity: memoryCapacityKiloBytes,
	}
	glog.V(4).Infof("Cpu capacity of node %s is %f core", node.Name, cpuCapacityCore)
	glog.V(4).Infof("Memory capacity of node %s is %f Kb", node.Name, memoryCapacityKiloBytes)
	nodeCpuCapacityCoreMetrics := metrics.NewEntityResourceMetric(task.NodeType, key, metrics.CPU,
		metrics.Capacity, cpuCapacityCore)
	nodeMemoryCapacityKiloBytesMetrics := metrics.NewEntityResourceMetric(task.NodeType, key,
		metrics.Memory, metrics.Capacity, memoryCapacityKiloBytes)
	nodeResourceMetrics = append(nodeResourceMetrics, nodeCpuCapacityCoreMetrics, nodeMemoryCapacityKiloBytesMetrics)

	// The provisioned resources have the same capacities.
	// cpuProvisioned and memoryProvisioned
	cpuProvisionedCapacityCore := cpuCapacityCore
	memoryProvisionedCapacityKiloBytes := memoryCapacityKiloBytes
	glog.V(4).Infof("Cpu provisioned capacity of node %s is %f core", node.Name, cpuProvisionedCapacityCore)
	glog.V(4).Infof("Memory provisioned capacity of node %s is %f Kb", node.Name, memoryProvisionedCapacityKiloBytes)
	nodeCpuProvisionedCapacityCoreMetrics := metrics.NewEntityResourceMetric(task.NodeType, key,
		metrics.CPUProvisioned, metrics.Capacity, cpuProvisionedCapacityCore)
	nodeMemoryProvisionedCapacityKiloBytesMetrics := metrics.NewEntityResourceMetric(task.NodeType, key,
		metrics.MemoryProvisioned, metrics.Capacity, memoryProvisionedCapacityKiloBytes)
	nodeResourceMetrics = append(nodeResourceMetrics, nodeCpuProvisionedCapacityCoreMetrics, nodeMemoryProvisionedCapacityKiloBytesMetrics)

	// Provisioned resource used is the sum of all the requested resource of all the pods running in the node.
	nodeCpuProvisionedUsedCore, nodeMemoryProvisionedUsedKiloBytes, err := util.GetNodeResourceRequestConsumption(m.nodePodMap[node.Name])
	if err != nil {
		return nil, fmt.Errorf("failed to get provision used metrics: %s", err)
	}
	glog.V(4).Infof("Cpu provisioned used of node %s is %f core", node.Name, nodeCpuProvisionedUsedCore)
	glog.V(4).Infof("Memory provisioned used of node %s is %f Kb", node.Name, nodeMemoryProvisionedUsedKiloBytes)
	nodeCpuProvisionedUsedCoreMetrics := metrics.NewEntityResourceMetric(task.NodeType, key,
		metrics.CPUProvisioned, metrics.Used, nodeCpuProvisionedUsedCore)
	nodeMemoryProvisionedUsedKiloBytesMetrics := metrics.NewEntityResourceMetric(task.NodeType, key,
		metrics.MemoryProvisioned, metrics.Used, nodeMemoryProvisionedUsedKiloBytes)
	nodeResourceMetrics = append(nodeResourceMetrics, nodeCpuProvisionedUsedCoreMetrics, nodeMemoryProvisionedUsedKiloBytesMetrics)

	return nodeResourceMetrics, nil
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
func (m *ClusterMonitor) findPodStates() {
	for _, podList := range m.nodePodMap {
		for _, pod := range podList {
			key := util.PodKeyFunc(pod)
			if key == "" {
				glog.Warning("Not a valid pod.")
				continue
			}
			podResourceMetrics, err := m.getPodResourceMetric(pod)
			if err != nil {
				glog.Errorf("Failed to find resource metric for pod %s: %s", key, err)
			}
			m.sink.AddNewMetricEntries(podResourceMetrics...)
		}
	}
}

// Get resource metrics of a single pod:
// 	CPU 			capacity, reservation
// 	memory 			capacity, reservation
//	CPUProvisioned 		used
//	memoryProvisioned 	used
//
// As we create one application per pod, here we also get the resource metrics for applications:
// 	CPU			capacity
//	memory			capacity
func (m *ClusterMonitor) getPodResourceMetric(pod *api.Pod) ([]metrics.Metric, error) {
	key := util.PodKeyFunc(pod)

	var podAndAppResourceMetrics []metrics.Metric

	podCpuCapacity, podMemoryCapacity, err := util.GetPodResourceLimits(pod)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource limits: %s", err)
	}
	if podCpuCapacity == 0 {
		if nodeCapacity, exist := m.nodeResourceCapacities[pod.Spec.NodeName]; exist {
			podCpuCapacity = nodeCapacity.cpuCapacity
		}
	}
	if podMemoryCapacity == 0 {
		if nodeCapacity, exist := m.nodeResourceCapacities[pod.Spec.NodeName]; exist {
			podMemoryCapacity = nodeCapacity.memoryCapacity
		}
	}

	podCpuRequest, podMemoryRequest, err := util.GetPodResourceRequest(pod)
	if err != nil {
		return nil, fmt.Errorf("failed to get resource requests: %s", err)
	}

	// Capacity
	cpuCapacityCore := podCpuCapacity
	memoryCapacityKiloBytes := podMemoryCapacity
	// cpu capacity
	glog.V(4).Infof("Cpu capacity of pod %s is %f core", key, cpuCapacityCore)
	podCpuCapacityCoreMetrics := metrics.NewEntityResourceMetric(task.PodType, key,
		metrics.CPU, metrics.Capacity, cpuCapacityCore)
	podAndAppResourceMetrics = append(podAndAppResourceMetrics, podCpuCapacityCoreMetrics)
	// memory capacity
	glog.V(4).Infof("Memory capacity of pod %s is %f Kb", key, memoryCapacityKiloBytes)
	podMemoryCapacityKiloBytesMetrics := metrics.NewEntityResourceMetric(task.PodType, key,
		metrics.Memory, metrics.Capacity, memoryCapacityKiloBytes)
	podAndAppResourceMetrics = append(podAndAppResourceMetrics, podMemoryCapacityKiloBytesMetrics)

	// Reservation
	cpuReservationCore := podCpuRequest
	memoryReservationKiloBytes := podMemoryRequest
	// cpu reservation
	glog.V(4).Infof("Cpu reservation of pod %s is %f core", key, cpuReservationCore)
	podCpuReservationCoreMetrics := metrics.NewEntityResourceMetric(task.PodType, key,
		metrics.CPU, metrics.Reservation, cpuReservationCore)
	podAndAppResourceMetrics = append(podAndAppResourceMetrics, podCpuReservationCoreMetrics)
	// memory reservation
	glog.V(4).Infof("Memory reservation of pod %s is %f Kb", key, memoryReservationKiloBytes)
	podMemoryReservationKiloBytesMetrics := metrics.NewEntityResourceMetric(task.PodType, key,
		metrics.Memory, metrics.Reservation, memoryReservationKiloBytes)
	podAndAppResourceMetrics = append(podAndAppResourceMetrics, podMemoryReservationKiloBytesMetrics)

	// Provision
	cpuProvisionedUsedCore := podCpuRequest
	memoryProvisionedUsedKiloBytes := podMemoryRequest
	// cpu provisioned used
	glog.V(4).Infof("Cpu provision used of pod %s is %f core", key, cpuProvisionedUsedCore)
	podCpuProvisionedUsedCoreMetrics := metrics.NewEntityResourceMetric(task.PodType, key,
		metrics.CPUProvisioned, metrics.Used, cpuProvisionedUsedCore)
	podAndAppResourceMetrics = append(podAndAppResourceMetrics, podCpuProvisionedUsedCoreMetrics)
	// memory provisioned used
	glog.V(4).Infof("Memory provision used of pod %s is %f core", key, memoryProvisionedUsedKiloBytes)
	podMemoryProvisionedUsedCoreMetrics := metrics.NewEntityResourceMetric(task.PodType, key,
		metrics.MemoryProvisioned, metrics.Used, memoryProvisionedUsedKiloBytes)
	podAndAppResourceMetrics = append(podAndAppResourceMetrics, podMemoryProvisionedUsedCoreMetrics)

	// Cpu and Memory capacity of an application is the same as hosting pod.
	// application cpu capacity
	glog.V(4).Infof("Cpu capacity of Application App-%s is %f core", key, cpuCapacityCore)
	applicationCpuCapacityCoreMetrics := metrics.NewEntityResourceMetric(task.ApplicationType, key,
		metrics.CPU, metrics.Capacity, cpuCapacityCore)
	podAndAppResourceMetrics = append(podAndAppResourceMetrics, applicationCpuCapacityCoreMetrics)
	// application memory capacity
	glog.V(4).Infof("Memory capacity of Application App-%s is %f Kb", key, memoryCapacityKiloBytes)
	applicationMemoryCapacityKiloBytesMetrics := metrics.NewEntityResourceMetric(task.ApplicationType, key,
		metrics.Memory, metrics.Capacity, memoryCapacityKiloBytes)
	podAndAppResourceMetrics = append(podAndAppResourceMetrics, applicationMemoryCapacityKiloBytesMetrics)

	return podAndAppResourceMetrics, nil
}
