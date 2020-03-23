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

	return &k8sDiscoveryWorker{
		id:                    wid,
		isFullDiscoveryWorker: isFullDiscoveryWorker,
		config:                config,
		monitoringWorker:      monitoringWorkerMap,
		sink:                  metricSink,
		globalMetricSink:      globalMetricSink,

		taskChan: make(chan *task.Task),
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
		glog.Errorf("%s", err)
		return task.NewTaskResult(worker.id, task.TaskFailed).WithErr(err)
	}

	if glog.V(4) {
		for _, node := range currTask.NodeList() {
			glog.Infof("%s : Node %s with %d pods\n", worker.id, node.Name, len(currTask.PodList()))
		}
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
				stopCh := make(chan struct{}, 1)
				defer close(finishCh)
				defer close(stopCh)
				defer wg.Done()

				w.ReceiveTask(resourceMonitorTask)
				t := time.NewTimer(timeoutSecond)
				go func() {
					glog.V(3).Infof("A %s monitoring worker from discovery worker %v is invoked for task %s.",
						w.GetMonitoringSource(), worker.id, resourceMonitorTask)
					// Assign task to monitoring worker.
					monitoringSink := w.Do()
					select {
					case <-stopCh:
						// glog.Infof("Calling thread: %s monitoring worker timeout!", w.GetMonitoringSource())
						return
					default:
					}
					// glog.Infof("%s has finished", w.GetMonitoringSource())
					t.Stop()
					// Don't do any filtering
					worker.sink.MergeSink(monitoringSink, nil)
					if worker.isFullDiscoveryWorker {
						// Merge metrics from global metrics sink into the metrics sink of each full discovery worker
						worker.sink.MergeSink(worker.globalMetricSink, nil)
					}
					// glog.Infof("send to finish channel %p", finishCh)
					finishCh <- struct{}{}
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
					stopCh <- struct{}{}
					// glog.Infof("%s stop", w.GetMonitoringSource())
					w.Stop()
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

	// Collect usages for pods in different namespaces and create used and capacity
	// metrics for the allocation resources of the namespaces and pods
	metricsCollector := NewMetricsCollector(worker, currTask)

	podMetricsCollection, err := metricsCollector.CollectPodMetrics()
	if err != nil {
		return task.NewTaskResult(worker.id, task.TaskFailed).WithErr(err)
	}
	namespaceMetricsCollection := metricsCollector.CollectNamespaceMetrics(podMetricsCollection)

	// Add the allocation metrics in the sink for the pods and the nodes
	worker.addPodAllocationMetrics(podMetricsCollection)

	// Collect allocation metrics for K8s controllers where usage values are aggregated from pods and capacity values
	// are from namespaces quota capacity
	controllerMetricsCollector := NewControllerMetricsCollector(worker, currTask)
	kubeControllers, err := controllerMetricsCollector.CollectControllerMetrics()
	if err != nil {
		return task.NewTaskResult(worker.id, task.TaskFailed).WithErr(err)
	}

	// Collect container replicas metrics with resource capacity value and multiple resource usage data points in
	// ContainerSpecMetrics list
	containerSpecMetricsCollector := NewContainerSpecMetricsCollector(worker.sink, currTask.PodList())
	containerSpecMetricsList, err := containerSpecMetricsCollector.CollectContainerSpecMetrics()
	if err != nil {
		return task.NewTaskResult(worker.id, task.TaskFailed).WithErr(err)
	}

	// Build Entity groups
	groupsCollector := NewGroupMetricsCollector(worker, currTask)
	entityGroups, _ := groupsCollector.CollectGroupMetrics()

	// Build DTOs after getting the metrics
	entityDTOs, podsWithDtos, err := worker.buildDTOs(currTask)
	if err != nil {
		return task.NewTaskResult(worker.id, task.TaskFailed).WithErr(err)
	}
	// 4. build entityDTOs for applications
	appEntityDTOs, podEntities, err := worker.buildAppDTOs(currTask, podsWithDtos)
	if err != nil {
		return task.NewTaskResult(worker.id, task.TaskFailed).WithErr(err)
	}
	entityDTOs = append(entityDTOs, appEntityDTOs...)

	// Uncomment this to dump the topology to a file for later use by the unit tests
	// util.DumpTopology(currTask, "test-topology.dat")

	// Task result with node, pod, container and application DTOs
	// pod entities with the associated app DTOs for creating service DTOs
	result := task.NewTaskResult(worker.id, task.TaskSucceeded).WithContent(entityDTOs)
	// In addition, return pod entities with the associated app DTOs for creating service DTOs
	result.WithPodEntities(podEntities)
	// return the namespace metrics created by this worker
	if len(namespaceMetricsCollection) > 0 {
		result.WithNamespaceMetrics(namespaceMetricsCollection)
	}
	// return container and pod groups created by this worker
	if len(entityGroups) > 0 {
		result.WithEntityGroups(entityGroups)
	}
	// Return k8s controllers created by this worker
	if len(kubeControllers) > 0 {
		result.WithKubeControllers(kubeControllers)
	}
	// Return ContainerSpecMetricsList with each individual container replica resource metrics data created by this worker
	if len(containerSpecMetricsList) > 0 {
		result.WithContainerSpecMetrics(containerSpecMetricsList)
	}
	return result
}

// =================================================================================================
func (worker *k8sDiscoveryWorker) addPodAllocationMetrics(podMetricsCollection PodMetricsByNodeAndNamespace) {
	etype := metrics.PodType
	for _, podsByQuotaMap := range podMetricsCollection {
		for _, podMetricsList := range podsByQuotaMap {
			for _, podMetrics := range podMetricsList {
				podKey := podMetrics.PodKey
				for resourceType, usedValue := range podMetrics.QuotaUsed {
					// Generate the metrics in the sink
					allocationUsedMetric := metrics.NewEntityResourceMetric(etype,
						podKey, resourceType,
						metrics.Used, usedValue)
					worker.sink.AddNewMetricEntries(allocationUsedMetric)
					glog.V(4).Infof("Created %s used for pod %s:  %f.",
						allocationUsedMetric.GetUID(), podMetrics.PodName, usedValue)
				}

				for resourceType, capValue := range podMetrics.QuotaCapacity {
					allocationCapMetric := metrics.NewEntityResourceMetric(etype,
						podKey, resourceType,
						metrics.Capacity, capValue)
					worker.sink.AddNewMetricEntries(allocationCapMetric)
					glog.V(4).Infof("Created %s capacity for pod %s: %f.",
						allocationCapMetric.GetUID(), podMetrics.PodName, capValue)
				}
			}
		}
	}
}

// ================================================================================================
// Build DTOs for nodes, pods, containers
func (worker *k8sDiscoveryWorker) buildDTOs(currTask *task.Task) ([]*proto.EntityDTO, []*api.Pod, error) {
	var result []*proto.EntityDTO

	// 0. setUp nodeName to nodeId mapping
	stitchingManager := stitching.NewStitchingManager(worker.config.stitchingPropertyType)

	// Node providers
	nodes := currTask.NodeList()
	cluster := currTask.Cluster()
	var volToPodsMap map[*api.PersistentVolume][]repository.PodVolume
	if cluster != nil {
		volToPodsMap = cluster.VolumeToPodsMap
	}

	for _, node := range nodes {
		if node != nil {
			glog.V(4).Infof("Setting up stitching properties for node : %v", node.Name)
			providerId := node.Spec.ProviderID
			stitchingManager.SetNodeUuidGetterByProvider(providerId)
			stitchingManager.StoreStitchingValue(node)
		}
	}

	// 1. build entityDTOs for nodes
	nodeEntityDTOBuilder := dtofactory.NewNodeEntityDTOBuilder(worker.sink, stitchingManager)
	nodeEntityDTOs, err := nodeEntityDTOBuilder.BuildEntityDTOs(nodes)
	if err != nil {
		glog.Errorf("Error while creating node entityDTOs: %v", err)
		// TODO Node discovery fails, directly return?
	}
	glog.V(3).Infof("Worker %s built %d node DTOs.", worker.id, len(nodeEntityDTOs))
	result = append(result, nodeEntityDTOs...)

	// 2. build entityDTOs for pods
	namespaceUIDMap := make(map[string]string)
	nodeNameUIDMap := make(map[string]string)
	if cluster != nil {
		namespaceUIDMap = cluster.NamespaceUIDMap // quota providers
		nodeNameUIDMap = cluster.NodeNameUIDMap   // node providers
	}
	pods := currTask.PodList()
	glog.V(3).Infof("Worker %s received %d pods.", worker.id, len(pods))

	podEntityDTOBuilder := dtofactory.NewPodEntityDTOBuilder(worker.sink, stitchingManager,
		nodeNameUIDMap, namespaceUIDMap)
	podEntityDTOs, err := podEntityDTOBuilder.BuildEntityDTOs(pods, volToPodsMap)
	if err != nil {
		glog.Errorf("Error while creating pod entityDTOs: %v", err)
	}
	result = append(result, podEntityDTOs...)
	glog.V(3).Infof("Worker %s built %d pod DTOs.", worker.id, len(podEntityDTOs))

	// Filter out pods that build DTO failed so
	// building container and app DTOs will not include them
	pods = excludeFailedPods(pods, podEntityDTOs)

	// 3. build entityDTOs for containers
	containerDTOBuilder := dtofactory.NewContainerDTOBuilder(worker.sink)
	containerDTOs, err := containerDTOBuilder.BuildDTOs(pods)
	// util.DumpTopology(containerDTOs, "test-topology.dat")
	if err != nil {
		glog.Errorf("Error while creating container entityDTOs: %v", err)
	}
	result = append(result, containerDTOs...)
	glog.V(3).Infof("Worker %s built %d container DTOs.", worker.id, len(containerDTOs))

	glog.V(2).Infof("Worker %s built total %d entityDTOs.", worker.id, len(result))

	// Return the DTOs created, also return the list of pods with valid DTOs
	return result, pods, nil
}

// Build App DTOs using the list of pods with valid DTOs
func (worker *k8sDiscoveryWorker) buildAppDTOs(currTask *task.Task, podsWithDtos []*api.Pod) ([]*proto.EntityDTO, []*repository.KubePod, error) {
	var result []*proto.EntityDTO

	cluster := currTask.Cluster()

	glog.V(4).Infof("%s: all pods %d, pods with dtos %d", worker.id, len(currTask.PodList()), len(podsWithDtos))
	// 4. build entityDTOs for application running on each container
	applicationEntityDTOBuilder := dtofactory.NewApplicationEntityDTOBuilder(worker.sink,
		cluster.PodClusterIDToServiceMap)

	var podEntities []*repository.KubePod
	for _, pod := range podsWithDtos {
		kubeNode := cluster.Nodes[pod.Spec.NodeName]
		kubePod := repository.NewKubePod(pod, kubeNode, cluster.Name)

		// Pod service Id
		service := cluster.PodClusterIDToServiceMap[kubePod.PodClusterId]
		if service != nil {
			kubePod.ServiceId = util.GetServiceClusterID(service)
			glog.V(4).Infof("Pod %s --> service %s", kubePod.PodClusterId, kubePod.ServiceId)
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

	glog.V(3).Infof("Worker %s built %d application DTOs.", worker.id, len(result))
	return result, podEntities, nil
}

// excludeFailedPods filters the pod list and excludes those pods not in the dto list
func excludeFailedPods(pods []*api.Pod, dtos []*proto.EntityDTO) []*api.Pod {
	m := map[string]*api.Pod{}
	for _, pod := range pods {
		m[string(pod.UID)] = pod
	}

	for i, dto := range dtos {
		pods[i] = m[dto.GetId()]
	}

	return pods[:len(dtos)]
}
