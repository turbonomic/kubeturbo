package master

import (
	"errors"
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	"github.com/golang/glog"
)

type ClusterMonitor struct {
	kubeClient *client.Client

	sink *metrics.EntityMetricSink

	nodeList []api.Node

	nodePodList map[string][]api.Pod
}

func NewClusterMonitor(config ClusterMonitorConfig) (*ClusterMonitor, error) {
	kubeClient, err := client.New(config.kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("Invalid API configuration: %v", err)
	}

	return &ClusterMonitor{
		kubeClient: kubeClient,
		sink:       metrics.NewEntityMetricSink(),
	}, nil
}

func (m *ClusterMonitor) AssignTask(task *task.Task) {
	m.nodeList = task.NodeList()
}

func (m *ClusterMonitor) Do() *metrics.EntityMetricSink {
	m.RetrieveClusterStat()

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

	m.nodePodList = m.groupPodsByNodeNames(m.nodeList)

	m.findNodeStates()
	m.findPodStates()

	return nil
}

// Get the cluster ID of the Kubernetes cluster.
// Use Kubernetes service UID as the key for cluster commodity
func (m *ClusterMonitor) findClusterID() error {
	svc, err := m.kubeClient.Services("default").Get("kubernetes")
	if err != nil {
		return "", err
	}
	kubernetesSvcID := string(svc.UID)
	clusterInfo := metrics.NewEntityStateMetric(task.ClusterType, "", metrics.Cluster, kubernetesSvcID)
	m.sink.AddNewMetricEntry(clusterInfo)

	return nil
}

func (m *ClusterMonitor) findNodeStates() {
	for _, node := range m.nodeList {
		key := util.NodeKeyFunc(node)
		if key == "" {
			glog.Warning("Invalid node")
			continue
		}

		nodeResourceMetrics := m.getNodeResourceMetrics(node)
		for _, nrm := range nodeResourceMetrics {
			m.sink.AddNewMetricEntry(nrm)
		}

		labelMetrics := parseNodeLabels(node)
		m.sink.AddNewMetricEntry(labelMetrics)
	}
}

// Get resource metrics of a single node:
// 	CPU 			capacity
// 	memory 			capacity
//	CPUProvisioned		capacity
//	memoryProvisioned	capacity
//	CPUProvisioned 		used
//	memoryProvisioned 	used
func (m *ClusterMonitor) getNodeResourceMetrics(node api.Node) []metrics.EntityResourceMetric {
	key := util.NodeKeyFunc(node)

	var nodeResourceMetrics []metrics.EntityResourceMetric

	// cpu
	cpuCapacityCore := float64(node.Status.Capacity.Cpu().Value())
	nodeCpuCapacityCoreMetrics := metrics.NewEntityResourceMetric(task.NodeType, key, metrics.CPU,
		metrics.Capacity, cpuCapacityCore)
	nodeResourceMetrics = append(nodeResourceMetrics, nodeCpuCapacityCoreMetrics)

	// memory
	memoryCapacityKiloBytes := float64(node.Status.Capacity.Memory().Value())
	nodeMemoryCapacityKiloBytesMetrics := metrics.NewEntityResourceMetric(task.NodeType, key,
		metrics.Memory, metrics.Capacity, memoryCapacityKiloBytes)
	nodeResourceMetrics = append(nodeResourceMetrics, nodeMemoryCapacityKiloBytesMetrics)

	// The provisioned resource has the same capacity.
	// cpuProvisioned
	cpuProvisionedCapacityCore := cpuCapacityCore
	nodeCpuProvisionedCapacityCoreMetrics := metrics.NewEntityResourceMetric(task.NodeType, key,
		metrics.CPUProvisioned, metrics.Capacity, cpuProvisionedCapacityCore)
	nodeResourceMetrics = append(nodeResourceMetrics, nodeCpuProvisionedCapacityCoreMetrics)

	// memoryProvisioned
	memoryProvisionedCapacityKiloBytes := memoryCapacityKiloBytes
	nodeMemoryProvisionedCapacityKiloBytesMetrics := metrics.NewEntityResourceMetric(task.NodeType, key,
		metrics.MemoryProvisioned, metrics.Capacity, memoryProvisionedCapacityKiloBytes)
	nodeResourceMetrics = append(nodeResourceMetrics, nodeMemoryProvisionedCapacityKiloBytesMetrics)

	// Provisioned resource used is the sum of all the requested resource of all the pods running in the node.
	var nodeCpuProvisionedUsedCore float64
	var nodeMemoryProvisionedUsedKiloBytes float64

	podList := m.nodePodList[node.Name]
	for _, pod := range podList {
		for _, container := range pod.Spec.Containers {
			request := container.Resources.Requests
			// Provisioned CPU and memory.
			ctnCpuProvisionedMilliCore := request.Cpu().MilliValue()
			nodeCpuProvisionedUsedCore += float64(ctnCpuProvisionedMilliCore) / util.MilliToUnit
			ctnMemoryProvisionedKiloBytes := request.Memory().MilliValue()
			nodeMemoryProvisionedUsedKiloBytes += float64(ctnMemoryProvisionedKiloBytes)
		}
	}
	// cpuProvisioned
	nodeCpuProvisionedUsedCoreMetrics := metrics.NewEntityResourceMetric(task.NodeType, key,
		metrics.CPUProvisioned, metrics.Capacity, nodeCpuProvisionedUsedCore)
	nodeResourceMetrics = append(nodeResourceMetrics, nodeCpuProvisionedUsedCoreMetrics)

	// memoryProvisioned
	nodeMemoryProvisionedUsedKiloBytesMetrics := metrics.NewEntityResourceMetric(task.NodeType, key,
		metrics.MemoryProvisioned, metrics.Capacity, nodeMemoryProvisionedUsedKiloBytes)
	nodeResourceMetrics = append(nodeResourceMetrics, nodeMemoryProvisionedUsedKiloBytesMetrics)

	return nodeResourceMetrics
}

// Parse the labels of a node and create one EntityStateMetric
func parseNodeLabels(node api.Node) metrics.EntityStateMetric {
	labelsMap := node.ObjectMeta.Labels
	if len(labelsMap) > 0 {
		var labels []string
		for key, value := range labelsMap {
			l := key + "=" + value
			glog.V(4).Infof("label for this Node is : %s", l)

			labels = append(labels, l)
		}
		return metrics.NewEntityStateMetric(task.PodType, util.NodeKeyFunc(node), metrics.Access, labels)
	} else {
		return metrics.EntityStateMetric{}
	}
}

func (m *ClusterMonitor) findPodStates() {
	for _, podList := range m.nodePodList {
		for _, pod := range podList {
			key := util.PodKeyFunc(pod)
			if key == "" {
				glog.Warning("Not a valid pod.")
				continue
			}
			podResourceMetrics := getPodResourceMetric(pod)
			for _, prm := range podResourceMetrics {
				m.sink.AddNewMetricEntry(prm)
			}

			selectorMetrics := parsePodNodeSelector(pod)
			m.sink.AddNewMetricEntry(selectorMetrics)
		}
	}
}

// Get resource metrics of a single pod:
// 	CPU 			capacity
// 	memory 			capacity
//	CPUProvisioned 		used
//	memoryProvisioned 	used
func getPodResourceMetric(pod api.Pod) []metrics.EntityResourceMetric {
	key := util.PodKeyFunc(pod)

	var memoryCapacityKiloBytes float64
	var cpuCapacityCore float64

	var cpuProvisionedUsedCore float64
	var memoryProvisionedUsedKiloBytes float64
	for _, container := range pod.Spec.Containers {
		limits := container.Resources.Limits
		request := container.Resources.Requests

		ctnMemoryCapacityKiloBytes := limits.Memory().MilliValue()
		if ctnMemoryCapacityKiloBytes == 0 {
			ctnMemoryCapacityKiloBytes = request.Memory().MilliValue()
		}
		ctnCpuCapacityMilliCore := limits.Cpu().MilliValue()
		memoryCapacityKiloBytes += float64(ctnMemoryCapacityKiloBytes)
		cpuCapacityCore += float64(ctnCpuCapacityMilliCore) / util.MilliToUnit

		// Provisioned CPU and memory.
		ctnCpuProvisionedMilliCore := request.Cpu().MilliValue()
		cpuProvisionedUsedCore += float64(ctnCpuProvisionedMilliCore) / util.MilliToUnit
		ctnMemoryProvisionedKiloBytes := request.Memory().MilliValue()
		memoryProvisionedUsedKiloBytes += float64(ctnMemoryProvisionedKiloBytes)
	}

	var podResourceMetrics []metrics.EntityResourceMetric
	// cpu capacity
	podCpuCapacityCoreMetrics := metrics.NewEntityResourceMetric(task.PodType, key,
		metrics.CPU, metrics.Capacity, cpuCapacityCore)
	podResourceMetrics = append(podResourceMetrics, podCpuCapacityCoreMetrics)
	// memory capacity
	podMemoryCapacityKiloBytesMetrics := metrics.NewEntityResourceMetric(task.NodeType, key,
		metrics.CPU, metrics.Capacity, memoryCapacityKiloBytes)
	podResourceMetrics = append(podResourceMetrics, podMemoryCapacityKiloBytesMetrics)

	// cpu provisioned used
	podCpuProvisionedUsedCoreMetrics := metrics.NewEntityResourceMetric(task.PodType, key,
		metrics.CPUProvisioned, metrics.Used, cpuProvisionedUsedCore)
	podResourceMetrics = append(podResourceMetrics, podCpuProvisionedUsedCoreMetrics)
	// memory provisioned used
	podMemoryProvisionedUsedCoreMetrics := metrics.NewEntityResourceMetric(task.PodType, key,
		metrics.MemoryProvisioned, metrics.Used, memoryProvisionedUsedKiloBytes)
	podResourceMetrics = append(podResourceMetrics, podMemoryProvisionedUsedCoreMetrics)

	return podResourceMetrics
}

func parsePodNodeSelector(pod api.Pod) metrics.EntityStateMetric {
	selectorMap := pod.Spec.NodeSelector
	if len(selectorMap) > 0 {
		var selectors []string
		for key, value := range selectorMap {
			s := key + "=" + value
			selectors = append(selectors, s)
		}
		return metrics.NewEntityStateMetric(task.PodType, util.PodKeyFunc(pod), metrics.Access, selectors)

	} else {
		return metrics.EntityStateMetric{}
	}
}

// Discover pods running on nodes specified in task.
func (m *ClusterMonitor) groupPodsByNodeNames(nodeList []api.Node) map[string][]api.Pod {
	groupedPods := make(map[string][]api.Pod)
	for _, node := range nodeList {
		nodeNonTerminatedPodsList, err := m.findNonTerminatedPods(node.Name)
		if err != nil {
			glog.Errorf("Failed to find non-ternimated pods in %s", node.Name)
			continue
		}
		groupedPods[node.Name] = nodeNonTerminatedPodsList.Items
	}
	return groupedPods
}

func (m *ClusterMonitor) findNonTerminatedPods(nodeName string) (*api.PodList, error) {
	fieldSelector, err := fields.ParseSelector("spec.nodeName=" + nodeName + ",status.phase!=" +
		string(api.PodSucceeded) + ",status.phase!=" + string(api.PodFailed))
	if err != nil {
		return nil, err
	}
	return m.kubeClient.Pods(api.NamespaceAll).List(api.ListOptions{FieldSelector: fieldSelector})
}
