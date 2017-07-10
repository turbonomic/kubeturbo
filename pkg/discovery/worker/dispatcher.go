package worker

import (
	"fmt"
	"math"

	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
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

func (d *Dispatcher) Init(c *ResultCollector) {
	for i := 0; i < d.config.workerCount; i++ {
		workerConfig := NewK8sDiscoveryWorkerConfig(d.config.probeConfig.StitchingPropertyType)
		for _, mc := range d.config.probeConfig.MonitoringConfigs {
			workerConfig.WithMonitoringWorkerConfig(mc)
		}
		// create workers
		discoveryWorker, err := NewK8sDiscoveryWorker(workerConfig)
		if err != nil {
			fmt.Errorf("failed to build discovery worker %s", err)
		}

		go discoveryWorker.RegisterAndRun(d, c)
	}
}

func (d *Dispatcher) RegisterWorker(worker *k8sDiscoveryWorker) {
	d.workerPool <- worker.taskChan
}

func (d *Dispatcher) Dispatch(nodes []*api.Node) int {
	// make sure when len(node) < workerCount, worker will receive at most 1 node to discover
	perTaskNodeLength := int(math.Ceil(float64(len(nodes)) / float64(d.config.workerCount)))
	glog.V(3).Infof("The number of nodes per task is: %d", perTaskNodeLength)
	assignedNodesCount := 0
	assignedWorkerCount := 0
	for assignedNodesCount+perTaskNodeLength <= len(nodes) {
		currNodes := nodes[assignedNodesCount : assignedNodesCount+perTaskNodeLength]
		currPods := d.config.clusterInfoScraper.GetRunningPodsOnNodes(currNodes)
		currTask := task.NewTask().WithNodes(currNodes).WithPods(currPods)
		d.assignTask(currTask)

		assignedNodesCount += perTaskNodeLength

		assignedWorkerCount++
	}
	if assignedNodesCount < len(nodes)-1 {
		currNodes := nodes[assignedNodesCount:]
		currTask := task.NewTask().WithNodes(currNodes)
		d.assignTask(currTask)

		assignedWorkerCount++
	}
	glog.V(3).Infof("Dispatched discovery task to %d workers", assignedWorkerCount)

	return assignedWorkerCount
}

func (d *Dispatcher) assignTask(t *task.Task) {
	// assignTask to a task channel of a worker.
	taskChannel := <-d.workerPool
	taskChannel <- t
}
