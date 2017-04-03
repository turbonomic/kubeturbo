package discovery

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/restclient"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	"github.com/golang/glog"
	"github.com/pborman/uuid"
)

const (
	milliToUnit = 1E3
)

type k8sDiscoveryWorkerConfig struct {
	kubeConfig             *restclient.Config
	monitoringWorkerConfig monitoring.MonitorWorkerConfig
	monitorSource          string
}

func NewK8sDiscoveryWorkerConfig(kubeConfig *restclient.Config, source string) *k8sDiscoveryWorkerConfig {

	return &k8sDiscoveryWorkerConfig{
		kubeConfig:    kubeConfig,
		monitorSource: source,
	}
}

func (c *k8sDiscoveryWorkerConfig) SetMonitoringWorkerConfig(config monitoring.MonitorWorkerConfig) *k8sDiscoveryWorkerConfig {
	c.monitoringWorkerConfig = config
	return c
}

type k8sDiscoveryWorker struct {
	id string

	//clusterAccessor probe.ClusterAccessor
	kubeClient *client.Client

	monitoringWorker monitoring.MonitoringWorker

	// Currently a worker only receives one task at a time.
	task task.WorkerTask
}

func NewK8sDiscoveryWorker(config *k8sDiscoveryWorkerConfig) (*k8sDiscoveryWorker, error) {
	id := uuid.NewUUID().String()

	kubeClient, err := client.New(config.kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("Invalid API configuration: %v", err)
	}

	monitoringWorker, err := monitoring.BuildMonitorWorker(config.monitorSource, config.monitoringWorkerConfig)
	if err != nil {
		return nil, fmt.Errorf("Invalid monitoring worker configuration: %v", err)
	}

	return &k8sDiscoveryWorker{
		id:               id,
		kubeClient:       kubeClient,
		monitoringWorker: monitoringWorker,
	}
}

func (worker *k8sDiscoveryWorker) AssignTask(task *task.WorkerTask) error {
	if worker.task != nil {
		return fmt.Errorf("The current worker %s has already been assigned a task and has not finished yet", worker.id)
	}
	worker.task = task
	return nil
}

// Worker start to working on task.
func (worker *k8sDiscoveryWorker) Do() {
	if worker.task == nil {
		glog.Error("No task has been assigned to current worker.")
		return
	}

	sink := metrics.NewMetricSink()
	sink.RegisterSinkForEntityType(task.NodeType)
	sink.RegisterSinkForEntityType(task.PodType)
	// Init metric sink. The same metric sink will be used by monitoring and topology discovery.
	worker.monitoringWorker.InitMetricSink(sink)
	// Assign task to monitoring worker.
	worker.monitoringWorker.AssignTask(worker.task)
	// TODO redefine the interface method, how to get data back.
	worker.monitoringWorker.RetrieveResourceStat()

	// Get all pods in the task scope
	pods := worker.findPodsRunningInTaskNodes()

	// Get Capacity for nodes, pods.
	findNodeResourceCapacity(worker.task.NodeList(), sink)
	findPodResourceCapacity(pods, sink)

	// Get Additional Attribute

	// Build EntityDTO

	// Send result

}

// Discover pods running on nodes specified in task.
func (worker *k8sDiscoveryWorker) findPodsRunningInTaskNodes() []*api.Pod {
	var podList []*api.Pod
	for _, node := range worker.task.NodeList() {
		nodeNonTerminatedPodsList, err := worker.findNonTerminatedPods(node.Name)
		if err != nil {
			glog.Errorf("Failed to find non-ternimated pods in %s", node.Name)
			continue
		}
		for _, pod := range nodeNonTerminatedPodsList {
			p := pod
			podList = append(podList, &p)
		}
	}
	return podList, nil
}

func (worker *k8sDiscoveryWorker) findNonTerminatedPods(nodeName string) ([]api.Pod, error) {
	fieldSelector, err := fields.ParseSelector("spec.nodeName=" + nodeName + ",status.phase!=" +
		string(api.PodSucceeded) + ",status.phase!=" + string(api.PodFailed))
	if err != nil {
		return "", err
	}
	return worker.kubeClient.Pods(api.NamespaceAll).List(api.ListOptions{FieldSelector: fieldSelector})
}

func findNodeResourceCapacity(sink *metrics.MetricSink, nodes []*api.Node) {
	for _, node := range nodes {
		if node == nil {
			glog.Error("node passed in is nil.")
		}

		nodeMetrics, err := sink.GetMetric(task.NodeType, util.NodeKeyFunc(node))
		if err != nil {
			glog.Warningf("Cannot find usage data for node %s", node.Name)
		}
		// cpu
		cpuCapacityCore := float64(node.Status.Capacity.Cpu().Value())
		nodeMetrics.SetMetric(metrics.CPU, metrics.Capacity, cpuCapacityCore)
		// memory
		memoryCapacityKiloBytes := float64(node.Status.Capacity.Memory().Value())
		nodeMetrics.SetMetric(metrics.Memory, metrics.Capacity, memoryCapacityKiloBytes)

		sink.UpdateMetricEntry(task.NodeType, util.NodeKeyFunc(node), nodeMetrics)
	}
}

//
func findPodResourceCapacity(sink *metrics.MetricSink, pods []*api.Pod) {
	for _, pod := range pods {
		if pod == nil {
			glog.Error("pod passed in is nil.")
		}

		podMetrics, err := sink.GetMetric(task.PodType, util.PodKeyFunc(pod))
		if err != nil {
			glog.Warningf("Cannot find usage data for pod %s", util.PodKeyFunc(pod))
		}

		var memoryCapacityKiloBytes float64
		var cpuCapacityCore float64
		for _, container := range pod.Spec.Containers {
			limits := container.Resources.Limits
			request := container.Resources.Requests

			ctnMemoryCapacityKiloBytes := limits.Memory().MilliValue()
			if ctnMemoryCapacityKiloBytes == 0 {
				ctnMemoryCapacityKiloBytes = request.Memory().MilliValue()
			}
			ctnCpuCapacityMilliCore := limits.Cpu().MilliValue()
			memoryCapacityKiloBytes += float64(ctnMemoryCapacityKiloBytes)
			cpuCapacityCore += float64(ctnCpuCapacityMilliCore) / milliToUnit
		}
		podMetrics.SetMetric(metrics.CPU, metrics.Capacity, cpuCapacityCore)
		podMetrics.SetMetric(metrics.Memory, metrics.Capacity, memoryCapacityKiloBytes)

		sink.UpdateMetricEntry(task.PodType, util.PodKeyFunc(pod), podMetrics)
	}
}
