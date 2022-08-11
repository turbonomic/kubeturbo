package worker

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang/glog"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
)

const (
	minTimeoutSec = 10   // 10 seconds
	maxTimeoutSec = 1200 // 20 minutes
)

type k8sDiscoveryWorkerConfig struct {
	// a collection of all configs for building different monitoring clients.
	// key: monitor type; value: monitor worker config.
	monitoringSourceConfigs map[types.MonitorType][]monitoring.MonitorWorkerConfig
	stitchingPropertyType   stitching.StitchingPropertyType
	monitoringWorkerTimeout time.Duration
	// Max metric samples to be collected by this discovery worker
	metricSamples int
}

func NewK8sDiscoveryWorkerConfig(sType stitching.StitchingPropertyType, timeoutSec, metricSamples int) *k8sDiscoveryWorkerConfig {
	var monitoringWorkerTimeout time.Duration
	if timeoutSec < minTimeoutSec {
		glog.Warningf("Invalid discovery timeout %v, set it to %v", timeoutSec, minTimeoutSec)
		monitoringWorkerTimeout = time.Second * minTimeoutSec
	} else if timeoutSec > maxTimeoutSec {
		glog.Warningf("Discovery timeout %v is larger than %v, set it to %v",
			timeoutSec, maxTimeoutSec, maxTimeoutSec)
		monitoringWorkerTimeout = time.Second * maxTimeoutSec
	} else {
		monitoringWorkerTimeout = time.Second * time.Duration(timeoutSec)
		glog.Infof("Discovery timeout is %v", monitoringWorkerTimeout)
	}
	return &k8sDiscoveryWorkerConfig{
		stitchingPropertyType:   sType,
		monitoringSourceConfigs: make(map[types.MonitorType][]monitoring.MonitorWorkerConfig),
		monitoringWorkerTimeout: monitoringWorkerTimeout,
		metricSamples:           metricSamples,
	}
}

// Add new monitoring worker config to the discovery worker config.
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

// k8sDiscoveryWorker receives a discovery task from dispatcher(DiscoveryClient). Then ask available monitoring workers
// to scrape metrics source and get topology information. Finally it builds entityDTOs and send back to DiscoveryClient.
type k8sDiscoveryWorker struct {
	// A UID of a discovery worker.
	id string
	// Whether this is full discovery worker, where EntityMetricSink needs to be reset in each discovery
	isFullDiscoveryWorker bool

	// config
	config *k8sDiscoveryWorkerConfig

	// a collection of all monitoring worker of different types.
	// key: monitoring types; value: monitoring worker instance.
	monitoringWorker map[types.MonitorType][]monitoring.MonitoringWorker

	// sink is a central place to store all the monitored data.
	sink *metrics.EntityMetricSink
	// Global entity metric sink to store all resource usage data samples scraped from kubelet
	globalMetricSink *metrics.EntityMetricSink

	stitchingManager *stitching.StitchingManager

	taskChan chan *task.Task
}

// Create new instance of k8sDiscoveryWorker.
// Also creates instances of MonitoringWorkers for each MonitorType.
func NewK8sDiscoveryWorker(config *k8sDiscoveryWorkerConfig, wid string, globalMetricSink *metrics.EntityMetricSink,
	isFullDiscoveryWorker bool) (*k8sDiscoveryWorker, error) {
	// id := uuid.NewUUID().String()
	if len(config.monitoringSourceConfigs) == 0 {
		return nil, errors.New("no monitoring source config found in config")
	}

	// Build all the monitoring worker based on configs.
	monitoringWorkerMap := make(map[types.MonitorType][]monitoring.MonitoringWorker)
	for monitorType, configList := range config.monitoringSourceConfigs {
		monitorList, exist := monitoringWorkerMap[monitorType]
		if !exist {
			monitorList = []monitoring.MonitoringWorker{}
		}
		for _, config := range configList {
			monitoringWorker, err := monitoring.BuildMonitorWorker(config.GetMonitoringSource(), config, isFullDiscoveryWorker)
			if err != nil {
				// TODO return?
				glog.Errorf("Failed to build monitoring worker configuration: %v", err)
				continue
			}
			monitorList = append(monitorList, monitoringWorker)
		}
		monitoringWorkerMap[monitorType] = monitorList
	}
	metricSink := globalMetricSink
	if isFullDiscoveryWorker {
		metricSink = metrics.NewEntityMetricSink().WithMaxMetricPointsSize(config.metricSamples)
	}

	var stitchingManager *stitching.StitchingManager
	if isFullDiscoveryWorker && config.stitchingPropertyType != "" {
		stitchingManager = stitching.NewStitchingManager(config.stitchingPropertyType)
	}

	return &k8sDiscoveryWorker{
		id:                    wid,
		isFullDiscoveryWorker: isFullDiscoveryWorker,
		config:                config,
		monitoringWorker:      monitoringWorkerMap,
		sink:                  metricSink,
		globalMetricSink:      globalMetricSink,
		stitchingManager:      stitchingManager,
		taskChan:              make(chan *task.Task),
	}, nil
}

// Register self with the Dispatcher and wait for the task to be submitted on the channel
func (worker *k8sDiscoveryWorker) RegisterAndRun(dispatcher *Dispatcher, collector *ResultCollector) {
	dispatcher.RegisterWorker(worker)
	for {
		// wait for a Task to be submitted
		select {
		case currTask := <-worker.taskChan:
			logLevel := glog.Level(2)
			if !worker.isFullDiscoveryWorker {
				logLevel = glog.Level(3)
			}

			glog.V(logLevel).Infof("Worker %s has received a task %v.", worker.id, currTask)
			result := worker.executeTask(currTask)
			if collector != nil {
				collector.ResultPool() <- result
			}
			glog.V(logLevel).Infof("Worker %s has finished the task %v.", worker.id, currTask)
			dispatcher.RegisterWorker(worker)
		}
	}
}

// Worker start to working on task.
func (worker *k8sDiscoveryWorker) executeTask(currTask *task.Task) *task.TaskResult {
	if currTask == nil {
		err := errors.New("no task has been assigned to current worker")
		glog.Errorf("Failed to execute task: %s", err)
		return task.NewTaskResult(worker.id, task.TaskFailed).WithErr(err)
	}

	if glog.V(4) {
		glog.Infof("Worker %s: Node %s with %d pods", worker.id, currTask.Node().Name, len(currTask.PodList()))
	}
	// wait group to make sure metrics scraping finishes.
	var wg sync.WaitGroup
	timeoutSecond := worker.config.monitoringWorkerTimeout
	var timeout bool
	// Resource monitoring
	resourceMonitorTask := currTask
	if worker.isFullDiscoveryWorker {
		// Reset the main sink
		worker.sink = metrics.NewEntityMetricSink().WithMaxMetricPointsSize(worker.config.metricSamples)
	}
	// if resourceMonitoringWorkers, exist := worker.monitoringWorker[types.ResourceMonitor]; exist {
	for _, resourceMonitoringWorkers := range worker.monitoringWorker {
		for _, rmWorker := range resourceMonitoringWorkers {
			wg.Add(1)
			go func(w monitoring.MonitoringWorker) {
				finishCh := make(chan struct{})
				timeoutCh := make(chan struct{}, 1)
				defer close(timeoutCh)
				defer wg.Done()

				w.ReceiveTask(resourceMonitorTask)
				t := time.NewTimer(timeoutSecond)
				go func() {
					glog.V(3).Infof("A %s monitoring worker from discovery worker %v is invoked for task %s.",
						w.GetMonitoringSource(), worker.id, resourceMonitorTask)
					// Assign task to monitoring worker.
					monitoringSink, err := w.Do()
					select {
					case <-timeoutCh:
						// glog.Infof("Calling thread: %s monitoring worker timeout!", w.GetMonitoringSource())
						return
					default:
					}
					// glog.Infof("%s has finished", w.GetMonitoringSource())
					t.Stop()
					if err == nil {
						// Don't do any filtering
						worker.sink.MergeSink(monitoringSink, nil)
						if worker.isFullDiscoveryWorker {
							// Merge metrics from global metrics sink into the metrics sink of each full discovery worker
							worker.sink.MergeSink(worker.globalMetricSink, nil)
						}

					}
					// glog.Infof("send to finish channel %p", finishCh)
					close(finishCh)
				}()

				// either finish as expected or timeout.
				select {
				case <-finishCh:
					// glog.Infof("Worker %s finished as expected.", w.GetMonitoringSource())
					return
				case <-t.C:
					glog.Errorf("%s monitoring worker from discovery worker %v exceeds the max time limit for "+
						"completing the task %s: %v", w.GetMonitoringSource(), worker.id, resourceMonitorTask, timeoutSecond)
					timeout = true
					timeoutCh <- struct{}{}
					// glog.Infof("%s stop", w.GetMonitoringSource())
					return
				}
			}(rmWorker)
		}
	}
	wg.Wait()

	if timeout {
		return task.NewTaskResult(worker.id, task.TaskFailed).WithErr(fmt.Errorf("discovery timeout"))
	}

	if !worker.isFullDiscoveryWorker {
		// Sampling discovery doesn't need to collect additional metrics
		return task.NewTaskResult(worker.id, task.TaskSucceeded)
	}

	if currTask.Cluster() == nil {
		err := errors.New("no cluster summary is set")
		glog.Errorf("Failed to execute task: %s", err)
		return task.NewTaskResult(worker.id, task.TaskFailed).WithErr(err)
	}

	// Note: We have confirmed that the cluster summary is properly set.
	// All the following calls should NOT return errors.

	// Collect usages for pods in different namespaces and create used and capacity
	// metrics for the quota resources of the namespaces and pods.
	metricsCollector := NewMetricsCollector(worker, currTask)
	podMetricsCollection := metricsCollector.CollectPodMetrics()
	namespaceMetricsCollection := metricsCollector.CollectNamespaceMetrics(podMetricsCollection)
	podVolumeMetricsCollection := metricsCollector.CollectPodVolumeMetrics()

	// Add the quota metrics in the sink for the pods and the nodes
	worker.addPodQuotaMetrics(podMetricsCollection)

	// Collect quota metrics for K8s controllers where usage values are aggregated from pods and capacity values
	// are from namespaces quota capacity
	kubeControllers := NewControllerMetricsCollector(worker, currTask).CollectControllerMetrics()

	// Collect container replicas metrics with resource capacity value and multiple resource usage data points in
	// ContainerSpecMetrics list
	// Only collect container spec metrics from running pods
	containerSpecMetricsList :=
		NewContainerSpecMetricsCollector(worker.sink, currTask.RunningPodList()).CollectContainerSpecMetrics()

	// Build Entity groups
	entityGroups := NewGroupMetricsCollector(worker, currTask).CollectGroupMetrics()

	// Build DTOs after getting the metrics
	entityDTOs, podEntities, sidecarContainerSpecs, podsWithVolumes := worker.buildEntityDTOs(currTask)

	// Uncomment this to dump the topology to a file for later use by the unit tests
	// util.DumpTopology(currTask, "test-topology.dat")

	// Task result with node, pod, container and application DTOs
	// pod entities with the associated app DTOs for creating service DTOs
	return task.NewTaskResult(worker.id, task.TaskSucceeded).
		// Node, Pod, Container and App DTOs
		WithContent(entityDTOs).
		// Pod entities with the associated app DTOs for creating service DTOs
		WithPodEntities(podEntities).
		// Namespace metrics
		WithNamespaceMetrics(namespaceMetricsCollection).
		// Pod and Container groups
		WithEntityGroups(entityGroups).
		// Workload Controllers
		WithKubeControllers(kubeControllers).
		// ContainerSpecMetricsList with each individual container replica resource metrics data
		WithContainerSpecMetrics(containerSpecMetricsList).
		// Volume metrics
		WithPodVolumeMetrics(podVolumeMetricsCollection).
		// List of sidecar containerSpecIds
		WithSidecarContainerSpecs(sidecarContainerSpecs).
		// List of pods with volumes
		WithPodsWithVolumes(podsWithVolumes)
}

func (worker *k8sDiscoveryWorker) addPodQuotaMetrics(podMetricsCollection PodMetricsByNodeAndNamespace) {
	etype := metrics.PodType
	for _, podsByQuotaMap := range podMetricsCollection {
		for _, podMetricsList := range podsByQuotaMap {
			for _, podMetrics := range podMetricsList {
				podKey := podMetrics.PodKey
				for resourceType, usedValue := range podMetrics.QuotaUsed {
					// Generate the metrics in the sink
					quotaUsedMetric := metrics.NewEntityResourceMetric(etype,
						podKey, resourceType,
						metrics.Used, usedValue)
					worker.sink.AddNewMetricEntries(quotaUsedMetric)
					glog.V(4).Infof("Created %s used for pod %s:  %f.",
						quotaUsedMetric.GetUID(), podMetrics.PodName, usedValue)
				}

				for resourceType, capValue := range podMetrics.QuotaCapacity {
					quotaCapMetric := metrics.NewEntityResourceMetric(etype,
						podKey, resourceType,
						metrics.Capacity, capValue)
					worker.sink.AddNewMetricEntries(quotaCapMetric)
					glog.V(4).Infof("Created %s capacity for pod %s: %f.",
						quotaCapMetric.GetUID(), podMetrics.PodName, capValue)
				}
			}
		}
	}
}

func (worker *k8sDiscoveryWorker) buildEntityDTOs(currTask *task.Task) ([]*proto.EntityDTO,
	[]*repository.KubePod, []string, []string) {
	var entityDTOs []*proto.EntityDTO
	// Build entity DTOs for nodes
	nodeDTOs := worker.buildNodeDTOs([]*api.Node{currTask.Node()})
	glog.V(3).Infof("Worker %s built %d node DTOs.", worker.id, len(nodeDTOs))
	if len(nodeDTOs) == 0 {
		return nil, nil, nil, nil
	}
	entityDTOs = append(entityDTOs, nodeDTOs...)
	// Build entity DTOs for pods
	podDTOs, runningPods, podWithVolumes := worker.buildPodDTOs(currTask)
	glog.V(3).Infof("Worker %s built %d pod DTOs.", worker.id, len(podDTOs))
	if len(podDTOs) == 0 {
		return entityDTOs, nil, nil, nil
	}
	entityDTOs = append(entityDTOs, podDTOs...)
	// Build entity DTOs for containers from running pods
	containerDTOs, sidecarContainerSpecs := worker.buildContainerDTOs(runningPods)
	glog.V(3).Infof("Worker %s built %d container DTOs.", worker.id, len(containerDTOs))
	if len(containerDTOs) > 0 {
		entityDTOs = append(entityDTOs, containerDTOs...)
	}
	// Build entity DTOs for applications from running pods
	appEntityDTOs, podEntities := worker.buildAppDTOs(runningPods, currTask.Cluster())
	glog.V(3).Infof("Worker %s built %d application DTOs.", worker.id, len(appEntityDTOs))
	if len(appEntityDTOs) > 0 {
		entityDTOs = append(entityDTOs, appEntityDTOs...)
	}
	glog.V(2).Infof("Worker %s built %d entity DTOs in total.", worker.id, len(entityDTOs))
	return entityDTOs, podEntities, sidecarContainerSpecs, podWithVolumes
}

func (worker *k8sDiscoveryWorker) buildNodeDTOs(nodes []*api.Node) []*proto.EntityDTO {
	// SetUp nodeName to nodeId mapping
	stitchingManager := worker.stitchingManager
	for _, node := range nodes {
		if node != nil {
			glog.V(4).Infof("Setting up stitching properties for node : %v", node.Name)
			providerId := node.Spec.ProviderID
			stitchingManager.SetNodeUuidGetterByProvider(providerId)
			stitchingManager.StoreStitchingValue(node)
		}
	}
	// Build entity DTOs for nodes
	return dtofactory.NewNodeEntityDTOBuilder(worker.sink, stitchingManager).BuildEntityDTOs(nodes)
}

// Build DTOs for running pods
func (worker *k8sDiscoveryWorker) buildPodDTOs(currTask *task.Task) ([]*proto.EntityDTO, []*api.Pod, []string) {
	glog.V(3).Infof("Worker %s received %d pods.", worker.id, len(currTask.PodList()))
	cluster := currTask.Cluster()
	if cluster == nil {
		// This should not happen, guard anyway
		glog.Errorf("Failed to build pod DTOs: cluster summary object is null for worker %s", worker.id)
		return nil, nil, nil
	}
	runningPodDTOs, pendingPodDTOs, podsWithVolumes := dtofactory.
		NewPodEntityDTOBuilder(worker.sink, worker.stitchingManager).
		// Node providers
		WithNodeNameUIDMap(cluster.NodeNameUIDMap).
		// Quota providers
		WithNameSpaceUIDMap(cluster.NamespaceUIDMap).
		// Pod to volume map
		WithPodToVolumesMap(cluster.PodToVolumesMap).
		// Running pods
		WithRunningPods(currTask.RunningPodList()).
		// Pending pods
		WithPendingPods(currTask.PendingPodList()).
		BuildEntityDTOs()

	var podDTOs []*proto.EntityDTO
	var runningPods []*api.Pod
	if len(runningPodDTOs) > 0 {
		podDTOs = append(podDTOs, runningPodDTOs...)
		// Filter out pods that build DTO failed so
		// building container and app DTOs will not include them
		runningPods = excludeFailedPods(currTask.RunningPodList(), runningPodDTOs)
	}
	if len(pendingPodDTOs) > 0 {
		podDTOs = append(podDTOs, pendingPodDTOs...)
	}
	return podDTOs, runningPods, podsWithVolumes
}

// Build DTOs for containers
func (worker *k8sDiscoveryWorker) buildContainerDTOs(runningPods []*api.Pod) ([]*proto.EntityDTO, []string) {
	return dtofactory.NewContainerDTOBuilder(worker.sink).BuildEntityDTOs(runningPods)
}

// Build App DTOs using the list of pods with valid DTOs
func (worker *k8sDiscoveryWorker) buildAppDTOs(
	runningPods []*api.Pod, cluster *repository.ClusterSummary) ([]*proto.EntityDTO, []*repository.KubePod) {
	var result []*proto.EntityDTO
	var podEntities []*repository.KubePod
	applicationEntityDTOBuilder := dtofactory.NewApplicationEntityDTOBuilder(worker.sink,
		cluster.PodClusterIDToServiceMap)
	for _, pod := range runningPods {
		kubeNode := cluster.NodeMap[pod.Spec.NodeName]
		kubePod := repository.NewKubePod(pod, kubeNode, cluster.Name)
		// Pod service Id
		service := cluster.PodClusterIDToServiceMap[kubePod.PodClusterId]
		if service != nil {
			kubePod.ServiceId = util.GetServiceClusterID(service)
			glog.V(4).Infof("Pod %s belong to service %s", kubePod.PodClusterId, kubePod.ServiceId)
		}
		// Pod apps
		appEntityDTOs, err := applicationEntityDTOBuilder.BuildEntityDTO(pod)
		if err != nil {
			glog.Errorf("Error while creating application entityDTOs: %v", err)
		}
		result = append(result, appEntityDTOs...)

		for _, dto := range appEntityDTOs {
			kubePod.ContainerApps[dto.GetId()] = dto
		}

		podEntities = append(podEntities, kubePod)
	}
	return result, podEntities
}

// excludeFailedPods filters the pod list and excludes those pods not in the dto list
func excludeFailedPods(pods []*api.Pod, dtos []*proto.EntityDTO) []*api.Pod {
	if pods == nil {
		return nil
	}
	m := map[string]*api.Pod{}
	for _, pod := range pods {
		m[string(pod.UID)] = pod
	}

	for i, dto := range dtos {
		pods[i] = m[dto.GetId()]
	}

	return pods[:len(dtos)]
}
