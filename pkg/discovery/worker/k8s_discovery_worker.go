package worker

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang/glog"
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
}

func NewK8sDiscoveryWorkerConfig(sType stitching.StitchingPropertyType, timeoutSec int) *k8sDiscoveryWorkerConfig {
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

	// config
	config *k8sDiscoveryWorkerConfig

	// a collection of all monitoring worker of different types.
	// key: monitoring types; value: monitoring worker instance.
	monitoringWorker map[types.MonitorType][]monitoring.MonitoringWorker

	// sink is a central place to store all the monitored data.
	sink *metrics.EntityMetricSink

	taskChan chan *task.Task
}

// Create new instance of k8sDiscoveryWorker.
// Also creates instances of MonitoringWorkers for each MonitorType.
func NewK8sDiscoveryWorker(config *k8sDiscoveryWorkerConfig, wid string) (*k8sDiscoveryWorker, error) {
	//id := uuid.NewUUID().String()
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
		id:               wid,
		config:           config,
		monitoringWorker: monitoringWorkerMap,
		sink:             metrics.NewEntityMetricSink(),

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
			glog.V(2).Infof("Worker %s has received a discovery task %v.", worker.id, currTask)
			result := worker.executeTask(currTask)
			collector.ResultPool() <- result
			glog.V(2).Infof("Worker %s has finished the discovery task %v.", worker.id, currTask)
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
	// Reset the main sink
	worker.sink = metrics.NewEntityMetricSink()
	//if resourceMonitoringWorkers, exist := worker.monitoringWorker[types.ResourceMonitor]; exist {
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
					glog.V(2).Infof("A %s monitoring worker from discovery worker %v is invoked for task %s.",
						w.GetMonitoringSource(), worker.id, resourceMonitorTask)
					// Assign task to monitoring worker.
					monitoringSink := w.Do()
					select {
					case <-stopCh:
						//glog.Infof("Calling thread: %s monitoring worker timeout!", w.GetMonitoringSource())
						return
					default:
					}
					//glog.Infof("%s has finished", w.GetMonitoringSource())
					t.Stop()
					// Don't do any filtering
					worker.sink.MergeSink(monitoringSink, nil)
					//glog.Infof("send to finish channel %p", finishCh)
					finishCh <- struct{}{}
				}()

				// either finish as expected or timeout.
				select {
				case <-finishCh:
					//glog.Infof("Worker %s finished as expected.", w.GetMonitoringSource())
					return
				case <-t.C:
					glog.Errorf("%s monitoring worker from discovery worker %v exceeds the max time limit for "+
						"completing the task %s: %v", w.GetMonitoringSource(), worker.id, resourceMonitorTask, timeoutSecond)
					timeout = true
					stopCh <- struct{}{}
					//glog.Infof("%s stop", w.GetMonitoringSource())
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

	// Collect usages for pods in different quotas and create used and capacity
	// metrics for the allocation resources of the nodes, quotas and pods
	metricsCollector := NewMetricsCollector(worker, currTask)

	podMetricsCollection, err := metricsCollector.CollectPodMetrics()
	if err != nil {
		return task.NewTaskResult(worker.id, task.TaskFailed).WithErr(err)
	}
	nodeMetricsCollection := metricsCollector.CollectNodeMetrics(podMetricsCollection)
	quotaMetricsCollection := metricsCollector.CollectQuotaMetrics(podMetricsCollection)

	// Add the allocation metrics in the sink for the pods and the nodes
	worker.addPodAllocationMetrics(podMetricsCollection)
	worker.addNodeAllocationMetrics(nodeMetricsCollection)

	// Build Entity groups
	groupsCollector := NewGroupMetricsCollector(worker, currTask)
	entityGroups, _ := groupsCollector.CollectGroupMetrics()

	// Build DTOs after getting the metrics
	entityDTOs, err := worker.buildDTOs(currTask)
	if err != nil {
		return task.NewTaskResult(worker.id, task.TaskFailed).WithErr(err)
	}
	// Uncomment this to dump the topology to a file for later use by the unit tests
	// util.DumpTopology(currTask, "test-topology.dat")

	// Task result with node and pod resource metrics, quota metrics and policy groups
	result := task.NewTaskResult(worker.id, task.TaskSucceeded).WithContent(entityDTOs)
	// return the quota metrics created by this worker
	if len(quotaMetricsCollection) > 0 {
		result.WithQuotaMetrics(quotaMetricsCollection)
	}
	if len(entityGroups) > 0 {
		result.WithEntityGroups(entityGroups)
	}
	return result
}

// =================================================================================================
func (worker *k8sDiscoveryWorker) addPodAllocationMetrics(podMetricsCollection PodMetricsByNodeAndQuota) {
	etype := metrics.PodType
	for _, podsByQuotaMap := range podMetricsCollection {
		for _, podMetricsList := range podsByQuotaMap {
			for _, podMetrics := range podMetricsList {
				podKey := podMetrics.PodKey
				for resourceType, usedValue := range podMetrics.AllocationBought {
					// Generate the metrics in the sink
					allocationUsedMetric := metrics.NewEntityResourceMetric(etype,
						podKey, resourceType,
						metrics.Used, usedValue)
					worker.sink.AddNewMetricEntries(allocationUsedMetric)
					glog.V(4).Infof("Created %s used for pod %s:  %f.",
						allocationUsedMetric.GetUID(), podMetrics.PodName, usedValue)
				}

				for resourceType, capValue := range podMetrics.ComputeCapacity {
					computeCapMetric := metrics.NewEntityResourceMetric(etype,
						podKey, resourceType,
						metrics.Capacity, capValue)
					worker.sink.AddNewMetricEntries(computeCapMetric)
					glog.V(4).Infof("Updated %s capacity for pod %s: %f.",
						computeCapMetric.GetUID(), podMetrics.PodName, capValue)
				}
			}
		}
	}
}

func (worker *k8sDiscoveryWorker) addNodeAllocationMetrics(nodeMetricsCollection NodeMetricsCollection) {
	entityType := metrics.NodeType
	for _, nodeMetrics := range nodeMetricsCollection {
		nodeKey := nodeMetrics.NodeKey
		for _, allocationResource := range metrics.QuotaResources {
			capValue := nodeMetrics.AllocationCap[allocationResource]
			allocationCapMetric := metrics.NewEntityResourceMetric(entityType, nodeKey,
				allocationResource, metrics.Capacity, capValue)
			worker.sink.AddNewMetricEntries(allocationCapMetric)

			usedValue := nodeMetrics.AllocationUsed[allocationResource]
			allocationUsedMetric := metrics.NewEntityResourceMetric(entityType, nodeKey,
				allocationResource, metrics.Used, usedValue)
			worker.sink.AddNewMetricEntries(allocationUsedMetric)

			glog.V(4).Infof("Created %s sold for node %s, used: %f, cap: %f.",
				allocationResource, nodeMetrics.NodeName, usedValue, capValue)
		}
	}
}

// ================================================================================================
func (worker *k8sDiscoveryWorker) buildDTOs(currTask *task.Task) ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO

	//0. setUp nodeName to nodeId mapping
	stitchingManager := stitching.NewStitchingManager(worker.config.stitchingPropertyType)

	// Node providers
	nodes := currTask.NodeList()
	cluster := currTask.Cluster()

	for _, node := range nodes {
		if node != nil {
			glog.V(3).Infof("The node we are parsing is : %v", node.Name)
			providerId := node.Spec.ProviderID
			stitchingManager.SetNodeUuidGetterByProvider(providerId)
			stitchingManager.StoreStitchingValue(node)
		}
	}

	//1. build entityDTOs for nodes
	nodeEntityDTOBuilder := dtofactory.NewNodeEntityDTOBuilder(worker.sink, stitchingManager)
	nodeEntityDTOs, err := nodeEntityDTOBuilder.BuildEntityDTOs(nodes)
	if err != nil {
		glog.Errorf("Error while creating node entityDTOs: %v", err)
		// TODO Node discovery fails, directly return?
	}
	glog.V(3).Infof("Worker %s builds %d node entityDTOs.", worker.id, len(nodeEntityDTOs))
	result = append(result, nodeEntityDTOs...)

	//2. build entityDTOs for pods
	quotaNameUIDMap := make(map[string]string)
	nodeNameUIDMap := make(map[string]string)
	if cluster != nil {
		quotaNameUIDMap = cluster.QuotaNameUIDMap // quota providers
		nodeNameUIDMap = cluster.NodeNameUIDMap   // node providers
	}
	pods := currTask.PodList()
	glog.V(3).Infof("Worker %s receives %d pods.", worker.id, len(pods))

	podEntityDTOBuilder := dtofactory.NewPodEntityDTOBuilder(worker.sink, stitchingManager,
		nodeNameUIDMap, quotaNameUIDMap)
	podEntityDTOs, err := podEntityDTOBuilder.BuildEntityDTOs(pods)
	if err != nil {
		glog.Errorf("Error while creating pod entityDTOs: %v", err)
	}
	result = append(result, podEntityDTOs...)
	glog.V(3).Infof("Worker %s builds %d pod entityDTOs.", worker.id, len(podEntityDTOs))

	// Filter out pods that build DTO failed so
	// building container and app DTOs will not include them
	pods = excludeFailedPods(pods, podEntityDTOs)

	//3. build entityDTOs for containers
	containerDTOBuilder := dtofactory.NewContainerDTOBuilder(worker.sink)
	containerDTOs, err := containerDTOBuilder.BuildDTOs(pods)
	//util.DumpTopology(containerDTOs, "test-topology.dat")
	if err != nil {
		glog.Errorf("Error while createing container entityDTOs: %v", err)
	}
	result = append(result, containerDTOs...)
	glog.V(3).Infof("Worker %s builds %d container entityDTOs.", worker.id, len(containerDTOs))

	//4. build entityDTOs for applications
	applicationEntityDTOBuilder := dtofactory.NewApplicationEntityDTOBuilder(worker.sink)
	appEntityDTOs, err := applicationEntityDTOBuilder.BuildEntityDTOs(pods)
	if err != nil {
		glog.Errorf("Error while creating application entityDTOs: %v", err)
	}
	result = append(result, appEntityDTOs...)
	glog.V(3).Infof("Worker %s builds %d application entityDTOs.", worker.id, len(appEntityDTOs))

	//result := task.NewTaskResult(worker.id, task.TaskSucceeded).WithContent(discoveryResult)
	glog.V(2).Infof("Worker %s builds %d entityDTOs.", worker.id, len(result))
	return result, nil
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
