package worker

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	api "k8s.io/api/core/v1"
	"math"
	"time"
)

type DispatcherConfig struct {
	clusterInfoScraper  *cluster.ClusterScraper
	probeConfig         *configs.ProbeConfig
	workerCount         int
	workerTimeoutSec    int
	samples             int
	samplingIntervalSec int
}

func NewDispatcherConfig(clusterInfoScraper *cluster.ClusterScraper, probeConfig *configs.ProbeConfig,
	workerCount, workerTimeoutSec, samples, samplingIntervalSec int) *DispatcherConfig {
	return &DispatcherConfig{
		clusterInfoScraper:  clusterInfoScraper,
		probeConfig:         probeConfig,
		workerCount:         workerCount,
		workerTimeoutSec:    workerTimeoutSec,
		samples:             samples,
		samplingIntervalSec: samplingIntervalSec,
	}
}

type Dispatcher struct {
	config           *DispatcherConfig
	workerPool       chan chan *task.Task
	globalMetricSink *metrics.EntityMetricSink
}

func NewDispatcher(config *DispatcherConfig, globalMetricSink *metrics.EntityMetricSink) *Dispatcher {
	return &Dispatcher{
		config: config,
		// TODO use maxWorker count for now. Improve in the future once we find a good way to get the number of task.
		// TODO If we allow worker number burst (# of workers > maxWorker), then the extra worker would block on registering. Or we use a threshold for burst number.
		workerPool:       make(chan chan *task.Task, config.workerCount),
		globalMetricSink: globalMetricSink,
	}
}

type SamplingDispatcher struct {
	Dispatcher
	// Timestamp when starting to schedule sampling discovery tasks in each full discovery cycle
	timestamp time.Time
	// Whether previous sampling discoveries are done
	samplingDone chan bool
	// Collected data samples since last full discovery
	collectedSamples int
}

func NewSamplingDispatcher(config *DispatcherConfig, globalMetricSink *metrics.EntityMetricSink) *SamplingDispatcher {
	return &SamplingDispatcher{
		Dispatcher: Dispatcher{
			config:           config,
			workerPool:       make(chan chan *task.Task, config.workerCount),
			globalMetricSink: globalMetricSink,
		},
		samplingDone:     make(chan bool),
		collectedSamples: 0,
	}
}

// Creates workerCount number of k8sDiscoveryWorker, each with multiple MonitoringWorkers for different types of monitorings/sources
// Each is registered with the Dispatcher
func (d *Dispatcher) Init(c *ResultCollector) {
	// Create discovery workers
	for i := 0; i < d.config.workerCount; i++ {
		// Create the worker instance
		workerConfig := NewK8sDiscoveryWorkerConfig(d.config.probeConfig.StitchingPropertyType, d.config.workerTimeoutSec, d.config.samples)
		for _, mc := range d.config.probeConfig.MonitoringConfigs {
			workerConfig.WithMonitoringWorkerConfig(mc)
		}
		wid := fmt.Sprintf("w%d", i)
		discoveryWorker, err := NewK8sDiscoveryWorker(workerConfig, wid, d.globalMetricSink, true)
		if err != nil {
			glog.Fatalf("failed to build discovery worker %s", err)
		}
		// Register the worker and let it wait on a separate thread for a task to be submitted
		go discoveryWorker.RegisterAndRun(d, c)
	}
}

func (d *Dispatcher) InitSamplingDiscoveryWorkers() {
	// Create sampling discovery workers
	// Sampling discovery only scrape kubelet which is very lightweight, so use 2 times of the full discovery worker count
	for i := 0; i < 2*d.config.workerCount; i++ {
		// Timeout of each sampling discovery worker is the given samplingIntervalSec to avoid goroutine pile up
		workerConfig := NewK8sDiscoveryWorkerConfig("", d.config.samplingIntervalSec, d.config.samples)
		for _, mc := range d.config.probeConfig.MonitoringConfigs {
			// Only monitor kubelet to collect additional resource usage data samples
			if mc.GetMonitoringSource() == types.KubeletSource {
				workerConfig.WithMonitoringWorkerConfig(mc)
			}
		}
		wid := fmt.Sprintf("w%d", i)
		discoveryWorker, err := NewK8sDiscoveryWorker(workerConfig, wid, d.globalMetricSink, false)
		if err != nil {
			glog.Fatalf("failed to build sampling discovery worker %s", err)
		}
		// Register the worker and let it wait on a separate thread for a task to be submitted
		// No need to collect results because sampled data are directly stored in globalEntityMetricSink
		go discoveryWorker.RegisterAndRun(d, nil)
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

// FinishSampling stops scheduling dispatcher to assign sampling discovery tasks.
func (d *SamplingDispatcher) FinishSampling() {
	if !d.timestamp.IsZero() {
		// Finish previous sampling discoveries
		d.samplingDone <- true
	}
}

// Create Task objects to discover multiple resource usage data samples for each node, and the pods and containers on that
// node from kubelet. Schedule dispatching tasks to the pool based on given sampling interval, tasks will be picked by
// the sampling discovery workers.
func (d *SamplingDispatcher) ScheduleDispatch(nodes []*api.Node) {
	glog.V(2).Info("Start scheduling sampling discovery tasks.")
	d.timestamp = time.Now()
	go func() {
		samplingInterval := time.Duration(d.config.samplingIntervalSec) * time.Second
		// Create a ticker to schedule dispatch based on given sampling interval
		ticker := time.NewTicker(samplingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-d.samplingDone:
				elapsedTime := time.Now().Sub(d.timestamp).Seconds()
				samples := int(math.Min(float64(d.config.samples), float64(d.collectedSamples)))
				glog.V(2).Infof("Collected %v usage data samples from kubelet in %v seconds since last full discovery.", samples, elapsedTime)
				d.collectedSamples = 0
				return
			case <-ticker.C:
				d.dispatchSamplingDiscoveries(nodes, samplingInterval)
			}
		}
	}()
}

// Dispatch sampling discovery tasks. Each task to discover one node will be picked up by an available sampling discovery
// worker. Set the timeout of finish assigning tasks of all nodes as given samplingInterval to avoid goroutine pile up.
func (d *SamplingDispatcher) dispatchSamplingDiscoveries(nodes []*api.Node, samplingInterval time.Duration) {
	finishCh := make(chan struct{})
	stopCh := make(chan struct{})
	defer close(finishCh)
	defer close(stopCh)
	t := time.NewTimer(samplingInterval)
	// Dispatch tasks to the pool, which will be picked up by available sampling discovery workers
	go func() {
		for i, node := range nodes {
			select {
			case <-stopCh:
				glog.Warningf("Dispatching sampling discovery tasks timeout. Collected data for %v out of %v nodes in this sampling cycle",
					i+1, len(nodes))
				return
			default:
			}
			currPods := d.config.clusterInfoScraper.GetRunningAndReadyPodsOnNode(node)
			currTask := task.NewTask().WithNode(node).WithPods(currPods)
			glog.V(3).Infof("Dispatching sampling discovery task %v", currTask)
			d.assignTask(currTask)
		}
		t.Stop()
		finishCh <- struct{}{}
	}()
	select {
	case <-finishCh:
		d.collectedSamples++
		return
	case <-t.C:
		glog.Errorf("Dispatching sampling discovery tasks for %v nodes with %v workers exceeds the max time limit: %v",
			len(nodes), d.config.workerCount, samplingInterval)
		stopCh <- struct{}{}
		return
	}
}

// Assign task to the k8sDiscoveryWorker
func (d *Dispatcher) assignTask(t *task.Task) {
	// assignTask to a task channel of a worker.
	taskChannel := <-d.workerPool // pick a free worker from the worker pool, when its channel frees up
	taskChannel <- t
}
