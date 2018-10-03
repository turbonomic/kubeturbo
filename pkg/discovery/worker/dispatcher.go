package worker

import (
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	api  "k8s.io/api/core/v1"
	"math"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

type DispatcherConfig struct {
	clusterInfoScraper *cluster.ClusterScraper
	probeConfig        *configs.ProbeConfig

	workerCount int
}

func NewDispatcherConfig(clusterInfoScraper *cluster.ClusterScraper, probeConfig *configs.ProbeConfig, workerCount int) *DispatcherConfig {
	return &DispatcherConfig{
		clusterInfoScraper: clusterInfoScraper,
		probeConfig:        probeConfig,
		workerCount:        workerCount,
	}
}

type Dispatcher struct {
	config     *DispatcherConfig
	workerPool chan chan *task.Task
}

func NewDispatcher(config *DispatcherConfig) *Dispatcher {
	return &Dispatcher{
		config: config,
		// TODO use maxWorker count for now. Improve in the future once we find a good way to get the number of task.
		// TODO If we allow worker number burst (# of workers > maxWorker), then the extra worker would block on registering. Or we use a threshold for burst number.
		workerPool: make(chan chan *task.Task, config.workerCount),
	}
}

// Creates workerCount number of k8sDiscoveryWorker, each with multiple MonitoringWorkers for different types of monitorings/sources
// Each is registered with the Dispatcher
func (d *Dispatcher) Init(c *ResultCollector) {
	// Create discovery workers
	for i := 0; i < d.config.workerCount; i++ {
		// Create the worker instance
		workerConfig := NewK8sDiscoveryWorkerConfig(d.config.probeConfig.StitchingPropertyType)
		for _, mc := range d.config.probeConfig.MonitoringConfigs {
			workerConfig.WithMonitoringWorkerConfig(mc)
		}
		wid := fmt.Sprintf("w%d", i)
		discoveryWorker, err := NewK8sDiscoveryWorker(workerConfig, wid)
		if err != nil {
			glog.Fatalf("failed to build discovery worker %s", err)
		}
		// Register the worker and let it wait on a separate thread for a task to be submitted
		go discoveryWorker.RegisterAndRun(d, c)
	}
}

// Register the k8sDiscoveryWorker and its monitoring workers
func (d *Dispatcher) RegisterWorker(worker *k8sDiscoveryWorker) {
	d.workerPool <- worker.taskChan
}

// Create Task objects for discovery and monitoring for each group of the nodes and pods
// Dispatch the task to the pool, task will be picked by the k8sDiscoveryWorker
// Receives the complete list of nodes in the cluster that are divided in groups and submitted as
// Tasks to the DiscoveryWorkers to carry out the discovery of the pods, containers and resources
func (d *Dispatcher) Dispatch(nodes []*api.Node, cluster *repository.ClusterSummary) int {

	// make sure when len(node) < workerCount, worker will receive at most 1 node to discover
	perTaskNodeLength := int(math.Ceil(float64(len(nodes)) / float64(d.config.workerCount)))
	glog.V(3).Infof("The number of nodes per task is: %d", perTaskNodeLength)
	assignedNodesCount := 0
	assignedWorkerCount := 0
	// Divide up the nodes into groups and assign each group to a separate task
	for assignedNodesCount+perTaskNodeLength <= len(nodes) {
		currNodes := nodes[assignedNodesCount : assignedNodesCount+perTaskNodeLength]

		currPods := d.config.clusterInfoScraper.GetRunningAndReadyPodsOnNodes(currNodes)

		currTask := task.NewTask().WithNodes(currNodes).WithPods(currPods).WithCluster(cluster)
		d.assignTask(currTask)

		assignedNodesCount += perTaskNodeLength

		assignedWorkerCount++
	}
	if assignedNodesCount < len(nodes) {
		currNodes := nodes[assignedNodesCount:]
		currPods := d.config.clusterInfoScraper.GetRunningAndReadyPodsOnNodes(currNodes)
		currTask := task.NewTask().WithNodes(currNodes).WithPods(currPods).WithCluster(cluster)
		d.assignTask(currTask)

		assignedWorkerCount++
	}
	glog.V(2).Infof("Dispatched discovery task to %d workers", assignedWorkerCount)

	return assignedWorkerCount
}

// Assign task to the k8sDiscoveryWorker
func (d *Dispatcher) assignTask(t *task.Task) {
	// assignTask to a task channel of a worker.
	taskChannel := <-d.workerPool // pick a free worker from the worker pool, when its channel frees up
	taskChannel <- t
}
