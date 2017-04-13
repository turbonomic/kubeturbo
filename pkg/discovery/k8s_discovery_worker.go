package discovery

import (
	"errors"
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/restclient"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/runtime"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"github.com/pborman/uuid"
)

type k8sDiscoveryWorkerConfig struct {
	kubeConfig *restclient.Config

	monitoringSourceConfigs map[types.MonitorType][]monitoring.MonitorWorkerConfig
}

func NewK8sDiscoveryWorkerConfig(kubeConfig *restclient.Config) *k8sDiscoveryWorkerConfig {
	return &k8sDiscoveryWorkerConfig{
		kubeConfig:              kubeConfig,
		monitoringSourceConfigs: make(map[types.MonitorType][]monitoring.MonitorWorkerConfig),
	}
}

func (c *k8sDiscoveryWorkerConfig) WithMonitoringWorkerConfig(config monitoring.MonitorWorkerConfig) *k8sDiscoveryWorkerConfig {
	monitorType := config.GetMonitorType()
	configs, exist := c.monitoringSourceConfigs[monitorType]
	if !exist {
		configs = []monitoring.MonitorWorkerConfig{}
	}

	configs = append(configs, config)
	c.monitoringSourceConfigs[monitorType] = configs

	return c
}

type k8sDiscoveryWorker struct {
	id string

	//clusterAccessor probe.ClusterAccessor
	kubeClient *client.Client

	monitoringWorker map[types.MonitorType][]monitoring.MonitoringWorker

	sink *metrics.EntityMetricSink

	// Currently a worker only receives one task at a time.
	task *task.Task
}

func NewK8sDiscoveryWorker(config *k8sDiscoveryWorkerConfig) (*k8sDiscoveryWorker, error) {
	id := uuid.NewUUID().String()

	kubeClient, err := client.New(config.kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("Invalid API configuration: %v", err)
	}

	if len(config.monitoringSourceConfigs) == 0 {
		return nil, errors.New("No monitoring source config found in config.")
	}

	// Build all the monitoring worker based on configs.
	monitoringWorkerMap := make(map[types.MonitorType][]monitoring.MonitoringWorker)
	for monitorType, configList := range config.monitoringSourceConfigs {
		monitorList, exist := monitoringWorkerMap[monitorType]
		if !exist {
			monitorList = []monitoring.MonitoringWorker{}
		}
		for _, config := range configList {
			monitoringWorker, err := monitoring.BuildMonitorWorker(config.GetMonitoringSource(), config)
			if err != nil {
				// TODO return?
				glog.Errorf("Failed to build monitoring worker configuration: %v", err)
				continue
			}
			monitorList = append(monitorList, monitoringWorker)
		}
		monitoringWorkerMap[monitorType] = monitorList
	}

	return &k8sDiscoveryWorker{
		id:               id,
		kubeClient:       kubeClient,
		monitoringWorker: monitoringWorkerMap,
		sink:             metrics.NewEntityMetricSink(),
	}, nil
}

func (worker *k8sDiscoveryWorker) AssignTask(task *task.Task) error {
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

	// Resource monitoring
	resourceMonitorTask := worker.task
	if resourceMonitoringWorkers, exist := worker.monitoringWorker[types.ResourceMonitor]; exist {
		for _, rmWorker := range resourceMonitoringWorkers {
			// Assign task to monitoring worker.
			rmWorker.AssignTask(resourceMonitorTask)
			monitoringSink := rmWorker.Do()
			// Don't do any filtering
			worker.sink.MergeSink(monitoringSink, nil)
		}
	}

	// Get all pods in the task scope
	//pods := worker.findPodsRunningInTaskNodes()

	// Topology monitoring
	clusterMonitorTask := worker.task
	if resourceMonitoringWorkers, exist := worker.monitoringWorker[types.StateMonitor]; exist {
		for _, smWorker := range resourceMonitoringWorkers {
			// Assign task to monitoring worker.
			smWorker.AssignTask(clusterMonitorTask)
			monitoringSink := smWorker.Do()
			// Don't do any filtering
			worker.sink.MergeSink(monitoringSink, nil)
		}
	}

	var discoveryResult []*proto.EntityDTO
	// Build EntityDTO
	nodeEntityDTOBuilder, err := dtofactory.GetK8sEntityDTOBuilder(task.NodeType, worker.sink)
	if err != nil {
		glog.Errorf("Error: %v", err)
		return
	}
	var nodes []runtime.Object
	for _, n := range worker.task.NodeList() {
		nodes = append(nodes, n)
	}
	nodeEntityDTOs, err := nodeEntityDTOBuilder.BuildEntityDTOs(nodes)
	if err != nil {
		glog.Errorf("Error while creating node entityDTOs: %v", err)
		// TODO Node discovery fails, directly return?
	}
	discoveryResult = append(discoveryResult, nodeEntityDTOs...)
	// Send result

}

// Discover pods running on nodes specified in task.
func (worker *k8sDiscoveryWorker) findPodsRunningInTaskNodes() []api.Pod {
	var podList []api.Pod
	for _, node := range worker.task.NodeList() {
		nodeNonTerminatedPodsList, err := worker.findNonTerminatedPods(node.Name)
		if err != nil {
			glog.Errorf("Failed to find non-ternimated pods in %s", node.Name)
			continue
		}
		podList = append(podList, nodeNonTerminatedPodsList.Items...)
	}
	return podList
}

func (worker *k8sDiscoveryWorker) findNonTerminatedPods(nodeName string) (*api.PodList, error) {
	fieldSelector, err := fields.ParseSelector("spec.nodeName=" + nodeName + ",status.phase!=" +
		string(api.PodSucceeded) + ",status.phase!=" + string(api.PodFailed))
	if err != nil {
		return nil, err
	}
	return worker.kubeClient.Pods(api.NamespaceAll).List(api.ListOptions{FieldSelector: fieldSelector})
}
