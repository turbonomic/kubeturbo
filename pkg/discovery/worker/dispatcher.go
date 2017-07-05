package worker

import (
	"fmt"
	"math"

	kubeClient "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/probe"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"

	"github.com/golang/glog"
)

type DispatcherConfig struct {
	kubeClient  *kubeClient.Clientset
	probeConfig *probe.ProbeConfig

	workerCount int
}

func NewDispatcherConfig(kubeClient *kubeClient.Clientset, probeConfig *probe.ProbeConfig, workerCount int) *DispatcherConfig {
	return &DispatcherConfig{
		kubeClient:  kubeClient,
		probeConfig: probeConfig,
		workerCount: workerCount,
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
		workerConfig := NewK8sDiscoveryWorkerConfig(d.config.kubeClient, d.config.probeConfig.StitchingPropertyType)
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

func (d *Dispatcher) WorkerPool() chan chan *task.Task {
	return d.workerPool
}

func (d *Dispatcher) Dispatch(nodes []*api.Node, workerCount int) int {
	// make sure when len(node) < workerCount, worker will receive at most 1 node to discover
	perTaskNodeLength := int(math.Ceil(float64(len(nodes)) / float64(workerCount)))
	glog.V(3).Infof("The number of nodes per task is: %d", perTaskNodeLength)
	assignedNodesCount := 0
	assignedWorkerCount := 0
	for assignedNodesCount+perTaskNodeLength <= len(nodes) {
		currNodes := nodes[assignedNodesCount : assignedNodesCount+perTaskNodeLength]
		currTask := task.NewTask().WithNodes(currNodes)
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
