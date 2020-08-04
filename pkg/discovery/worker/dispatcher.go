package worker

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	api "k8s.io/api/core/v1"
)

type DispatcherConfig struct {
	clusterInfoScraper *cluster.ClusterScraper
	probeConfig        *configs.ProbeConfig
	workerCount        int
	workerTimeoutSec   int
}

func NewDispatcherConfig(clusterInfoScraper *cluster.ClusterScraper, probeConfig *configs.ProbeConfig,
	workerCount int, workerTimeoutSec int) *DispatcherConfig {
	return &DispatcherConfig{
		clusterInfoScraper: clusterInfoScraper,
		probeConfig:        probeConfig,
		workerCount:        workerCount,
		workerTimeoutSec:   workerTimeoutSec,
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
		workerConfig := NewK8sDiscoveryWorkerConfig(d.config.probeConfig.StitchingPropertyType, d.config.workerTimeoutSec)
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
	// Return the free worker to the pool
	d.workerPool <- worker.taskChan
}

// Create Task objects for discovery and monitoring for each node, and the pods and containers on that node
// Dispatch the task to the pool, task will be picked by the k8sDiscoveryWorker
func (d *Dispatcher) Dispatch(nodes []*api.Node, cluster *repository.ClusterSummary) int {
	go func() {
		for _, node := range nodes {
			currPods := d.config.clusterInfoScraper.GetRunningAndReadyPodsOnNode(node)
			// Save the node to pods map in the cluster summary
			cluster.SetRunningPodsOnNode(node, currPods)
			currTask := task.NewTask().WithNode(node).WithPods(currPods).WithCluster(cluster)
			glog.V(2).Infof("Dispatching task %v", currTask)
			d.assignTask(currTask)
		}
	}()
	return len(nodes)
}

// Assign task to the k8sDiscoveryWorker
func (d *Dispatcher) assignTask(t *task.Task) {
	// assignTask to a task channel of a worker.
	taskChannel := <-d.workerPool // pick a free worker from the worker pool, when its channel frees up
	taskChannel <- t
}
