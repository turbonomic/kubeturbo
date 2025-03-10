package worker

import (
	"fmt"
	"sync/atomic"
	"time"

	api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/compliance/podaffinity"
	"github.ibm.com/turbonomic/kubeturbo/pkg/resourcemapping"
)

type DispatcherConfig struct {
	clusterInfoScraper  *cluster.ClusterScraper
	probeConfig         *configs.ProbeConfig
	workerCount         int
	workerTimeoutSec    int
	samples             int
	samplingIntervalSec int
	clusterKeyInjected  string
	commodityConfig     *dtofactory.CommodityConfig
	ormHandler          *resourcemapping.ORMHandler
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

func (config *DispatcherConfig) WithClusterKeyInjected(clusterKeyInjected string) *DispatcherConfig {
	config.clusterKeyInjected = clusterKeyInjected
	return config
}

func (config *DispatcherConfig) WithORMHandler(ormHandler *resourcemapping.ORMHandler) *DispatcherConfig {
	config.ormHandler = ormHandler
	return config
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
	finishSampling chan bool
	// Collected data samples since last full discovery
	collectedSamples atomic.Int32
}

func NewSamplingDispatcher(config *DispatcherConfig, globalMetricSink *metrics.EntityMetricSink) *SamplingDispatcher {
	return &SamplingDispatcher{
		Dispatcher: Dispatcher{
			config:           config,
			workerPool:       make(chan chan *task.Task, config.workerCount),
			globalMetricSink: globalMetricSink,
		},
		finishSampling: make(chan bool),
	}
}

// Init creates workerCount number of k8sDiscoveryWorker, each with multiple MonitoringWorkers for different types of monitorings/sources
// Each is registered with the Dispatcher
func (d *Dispatcher) Init(c *ResultCollector) {
	// Create discovery workers
	for i := 0; i < d.config.workerCount; i++ {
		// Create the worker instance
		workerConfig := NewK8sDiscoveryWorkerConfig(d.config.probeConfig, d.config.probeConfig.StitchingPropertyType, d.config.workerTimeoutSec, d.config.samples).
			WithClusterKeyInjected(d.config.clusterKeyInjected)
		for _, mc := range d.config.probeConfig.MonitoringConfigs {
			workerConfig.WithMonitoringWorkerConfig(mc)
		}
		wid := fmt.Sprintf("w%d", i)
		discoveryWorker, err := NewK8sDiscoveryWorker(workerConfig, wid, d.globalMetricSink, true)
		if err != nil {
			glog.Fatalf("failed to build discovery worker %s", err)
		}
		// Register the worker and let it wait on a separate thread for a task to be submitted
		discoveryWorker.WithORMHandler(d.config.ormHandler)
		go discoveryWorker.RegisterAndRun(d, c)
	}
}

func (d *Dispatcher) InitSamplingDiscoveryWorkers() {
	// Create sampling discovery workers
	// Sampling discovery only scrape kubelet which is very lightweight, so use 2 times of the full discovery worker count
	for i := 0; i < 2*d.config.workerCount; i++ {
		// Timeout of each sampling discovery worker is the given samplingIntervalSec to avoid goroutine pile up
		workerConfig := NewK8sDiscoveryWorkerConfig(d.config.probeConfig, "", d.config.samplingIntervalSec, d.config.samples)
		for _, mc := range d.config.probeConfig.MonitoringConfigs {
			// Only monitor kubelet to collect additional resource usage data samples
			if mc.GetMonitoringSource() == types.KubeletSource ||
				// DummySource is for unit test only
				mc.GetMonitoringSource() == types.DummySource {
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
		discoveryWorker.WithORMHandler(d.config.ormHandler)
		go discoveryWorker.RegisterAndRun(d, nil)
	}
}

// RegisterWorker registers the k8sDiscoveryWorker and its monitoring workers
func (d *Dispatcher) RegisterWorker(worker *k8sDiscoveryWorker) {
	// Return the free worker to the pool
	d.workerPool <- worker.taskChan
}

// Dispatch creates Task objects for discovery and monitoring of each node, and the pods and containers on that node
// Tasks are dispatched to a pool, and will be picked by the k8sDiscoveryWorker
func (d *Dispatcher) Dispatch(nodes []*api.Node, nodesPods map[string][]string, podsWithAffinities,
	otherSpreadPods sets.String, hostnameSpreadWorkloads map[string]sets.String, cluster *repository.ClusterSummary,
	node2nodegroup map[string]sets.String, podNonHostnameAntiTermTopologyKeys map[string]sets.String,
	topologySpreadConstraintNodesToPods map[string]sets.String, affinityMapper *podaffinity.AffinityMapper) int {
	go func() {
		for _, node := range nodes {
			runningPods := cluster.GetRunningPodsOnNode(node)
			pendingPods := cluster.GetPendingPodsOnNode(node)
			currTask := task.NewTask().
				WithNode(node).
				WithRunningPods(runningPods).
				WithPendingPods(pendingPods).
				WithCluster(cluster).
				WithNodesPods(nodesPods).
				WithPodsWithLabelBasedAffinities(podsWithAffinities).
				WithHostnameSpreadWorkloads(hostnameSpreadWorkloads).
				WithOtherSpreadPods(otherSpreadPods).
				WithPodsToControllers(cluster.PodToControllerMap).
				WithNode2NodeGroup(node2nodegroup).
				WithPodNonHostnameAntiTermTopologyKeys(podNonHostnameAntiTermTopologyKeys).
				WithTopologySpreadConstraintNodesToPods(topologySpreadConstraintNodesToPods).
				WithAffinityMapper(affinityMapper).
				WithORMHandler(d.config.ormHandler)
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
		d.finishSampling <- true
	}
}

// ScheduleSamplingDiscoveries creates Task objects to discover multiple resource usage data samples for each node,
// and the pods and containers on that node from kubelet.
// Schedule sampling tasks to the pool based on given sampling interval, tasks will be picked by the sampling
// discovery workers.
func (d *SamplingDispatcher) ScheduleSamplingDiscoveries(nodes []*api.Node) {
	glog.V(2).Infof("Start scheduling sampling discovery tasks to %v nodes.", len(nodes))
	d.timestamp = time.Now()
	go func() {
		samplingInterval := time.Duration(d.config.samplingIntervalSec) * time.Second
		// Create a ticker to schedule dispatch based on given sampling interval
		ticker := time.NewTicker(samplingInterval)
		defer ticker.Stop()
		for {
			// Dispatch a round of sampling IMMEDIATELY. This may incur additional overhead for instant data, but is
			// critical for cumulative data to make sure we do not miss data from any sampling window
			d.dispatchSamplingDiscoveries(nodes, samplingInterval)
			select {
			case <-d.finishSampling:
				// Sampling is stopped by main discovery
				elapsedTime := time.Now().Sub(d.timestamp).Seconds()
				collectedSamples := d.GetCollectedSamples()
				if d.config.samples < collectedSamples {
					collectedSamples = d.config.samples
				}
				glog.V(2).Infof("Completed %v sampling cycles (%v nodes per cycle) in %v seconds since "+
					"last full discovery.", collectedSamples, len(nodes), elapsedTime)
				// Reset the counter
				d.collectedSamples.Store(0)
				return
			case <-ticker.C:
				// Time for another round of sampling
				continue
			}
		}
	}()
}

// GetCollectedSamples get the number collected samples. For testing only.
func (d *SamplingDispatcher) GetCollectedSamples() int {
	return int(d.collectedSamples.Load())
}

// Dispatch sampling discovery tasks. Each task to discover one node will be picked up by an available sampling discovery
// worker. Set the timeout of finish assigning tasks of all nodes as given samplingInterval to avoid goroutine pile up.
func (d *SamplingDispatcher) dispatchSamplingDiscoveries(nodes []*api.Node, samplingInterval time.Duration) {
	// done channel indicates that a round of sampling is done
	done := make(chan struct{})
	// abort channel indicates that a round of sampling is aborted due to timeout
	abort := make(chan struct{})
	t := time.NewTimer(samplingInterval)
	defer t.Stop()
	// Dispatch tasks to the pool, which will be picked up by available sampling discovery workers
	go func() {
		for i, node := range nodes {
			select {
			case <-abort:
				glog.Warningf("Timed out dispatching sampling discovery tasks to %v out of %v nodes in this sampling cycle.",
					i+1, len(nodes))
				return
			default:
			}
			currTask := task.NewTask().WithNode(node)
			glog.V(4).Infof("Dispatching sampling discovery task %v", currTask)
			d.assignTask(currTask)
		}
		// Successfully dispatched sampling tasks to all nodes, notify the parent function
		close(done)
	}()
	select {
	case <-done:
		collectedSamples := d.collectedSamples.Add(1)
		glog.V(3).Infof("Sampling %v on %v nodes have all been dispatched.", collectedSamples, len(nodes))
		return
	case <-t.C:
		glog.Warningf("Timed out (in %v) while dispatching sampling discovery tasks to %v nodes with %v workers.",
			samplingInterval, len(nodes), 2*d.config.workerCount)
		// Timeout occurred, notify the child goroutine to quit
		close(abort)
	}
}

// Assign task to the k8sDiscoveryWorker
func (d *Dispatcher) assignTask(task *task.Task) {
	// assignTask to a task channel of a worker.
	// Worker pool is channel of channels.
	workerChannel := <-d.workerPool // pick a free worker from the worker pool, when its channel frees up
	workerChannel <- task
}
