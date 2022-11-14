package worker

import (
	"flag"
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"math"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	v1 "k8s.io/api/core/v1"

	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
)

func init() {
	flag.Set("alsologtostderr", fmt.Sprintf("%t", true))
	var logLevel string
	flag.StringVar(&logLevel, "logLevel", "2", "test")
	flag.Lookup("v").Value.Set(logLevel)
}

func TestDispatcher_DispatchMimic(t *testing.T) {

	nodeNum := 5
	workerCount := 4

	receiveNum := 0
	queue := make(chan int)

	seg := int(math.Ceil(float64(nodeNum) / (float64(workerCount))))
	fmt.Printf("seg = %d\n", seg)

	go func() {
		defer close(queue)

		assignedNum := 0
		nodes := make([]int, nodeNum)
		for i := range nodes {
			nodes[i] = i
		}

		for assignedNum+seg <= nodeNum {
			task := nodes[assignedNum : assignedNum+seg]

			for _, t := range task {
				queue <- t
			}
			assignedNum += seg
		}

		//if assignedNum < nodeNum - 1 {
		if assignedNum < nodeNum {
			task := nodes[assignedNum:]
			for _, t := range task {
				queue <- t
			}
		}
	}()

	a := 0
	for t := range queue {
		receiveNum += 1
		a += t
	}

	fmt.Printf("sentNum=%d, receivedNum=%d, a=%d\n", nodeNum, receiveNum, a)
	if receiveNum != nodeNum {
		t.Errorf("%d Vs. %d", receiveNum, nodeNum)
	}
}

func getDispatcherAndCollector(workerCount, taskRunTimeSec int) (*Dispatcher, *ResultCollector) {
	clusterScraper := &cluster.ClusterScraper{}
	probeConfig := &configs.ProbeConfig{
		MonitoringConfigs: []monitoring.MonitorWorkerConfig{&monitoring.DummyMonitorConfig{
			TaskRunTime: taskRunTimeSec,
		}},
		StitchingPropertyType: "IP",
	}
	commodityConfig := dtofactory.DefaultCommodityConfig()
	dispatcherConfig := NewDispatcherConfig(clusterScraper, probeConfig, workerCount,
		10, 1, 1, commodityConfig)
	dispatcher := NewDispatcher(dispatcherConfig, metrics.NewEntityMetricSink())
	resultCollector := NewResultCollector(workerCount * 2)
	dispatcher.Init(resultCollector)
	return dispatcher, resultCollector
}

// Test dispatching 1000 tasks with 100 workers, each worker run for 1 second, expect the test to finish in 10 seconds
func TestDispatcher_Dispatch_1000_Tasks_With_100_Workers(t *testing.T) {
	taskCount := 1000
	workerCount := 100
	taskRunTimeSec := 1
	expectedRunSec := taskCount / workerCount * taskRunTimeSec
	timeout := time.After(time.Duration(2*expectedRunSec) * time.Second)
	done := make(chan bool)
	start := time.Now()
	var result *DiscoveryResult
	go func() {
		// Actual testing
		kubeCluster := repository.NewKubeCluster("MyCluster", []*v1.Node{})
		clusterSummary := repository.CreateClusterSummary(kubeCluster)
		dispatcher, resultCollector := getDispatcherAndCollector(workerCount, taskRunTimeSec)
		var nodes = make([]struct{}, taskCount)
		go func() {
			for range nodes {
				node := new(v1.Node)
				currTask := task.NewTask().WithNode(node).WithCluster(clusterSummary)
				dispatcher.assignTask(currTask)
			}
		}()
		result = resultCollector.Collect(len(nodes))
		done <- true
	}()

	select {
	case <-timeout:
		t.Fatal("Test didn't finish in time")
	case <-done:
	}
	assert.Equal(t, taskCount, result.SuccessCount)
	assert.True(t, time.Since(start) > time.Duration(expectedRunSec)*time.Second)
}

// Test dispatching 4 tasks with 2 workers, each worker run for 60 second, expect the test to timeout in 20 seconds
func TestDispatcher_Dispatch_With_Task_Timeout(t *testing.T) {
	taskCount := 4
	workerCount := 2
	taskRunTimeSec := 60
	expectedRunSec := taskCount / workerCount * 10
	timeout := time.After(time.Duration(2*expectedRunSec) * time.Second)
	done := make(chan bool)
	var result *DiscoveryResult
	go func() {
		// Actual testing
		dispatcher, resultCollector := getDispatcherAndCollector(workerCount, taskRunTimeSec)
		var nodes = make([]struct{}, taskCount)
		go func() {
			for range nodes {
				currTask := task.NewTask()
				dispatcher.assignTask(currTask)
			}
		}()
		result = resultCollector.Collect(len(nodes))
		// Sleep a little to give chance to defer funcs to be called
		time.Sleep(time.Second * 5)
		done <- true
	}()

	select {
	case <-timeout:
		t.Fatal("Test didn't finish in time")
	case <-done:
	}
	assert.Equal(t, taskCount, result.ErrorCount)
}

func getSamplingDispatcherAndCollector(workerCount, workerTimeout, taskRunTimeSec int) (*SamplingDispatcher, *ResultCollector) {
	clusterScraper := &cluster.ClusterScraper{}
	probeConfig := &configs.ProbeConfig{
		MonitoringConfigs: []monitoring.MonitorWorkerConfig{&monitoring.DummyMonitorConfig{
			TaskRunTime: taskRunTimeSec,
		}},
		StitchingPropertyType: "IP",
	}
	commodityConfig := dtofactory.DefaultCommodityConfig()
	dispatcherConfig := NewDispatcherConfig(clusterScraper, probeConfig, workerCount, workerTimeout,
		1, 1, commodityConfig)
	dispatcher := NewSamplingDispatcher(dispatcherConfig, metrics.NewEntityMetricSink())
	resultCollector := NewResultCollector(workerCount * 2)
	dispatcher.Init(resultCollector)
	return dispatcher, resultCollector
}

func TestSamplingDispatcher_Dispatch_1000_Tasks_With_100_Workers(t *testing.T) {
	taskCount := 1000
	workerCount := 100
	workerTimeout := 10
	taskRunTimeSec := 1 //shorter than workerTimeout, the task will finish before worker timeout
	expectedRunSec := taskCount / workerCount * taskRunTimeSec
	testTimeout := time.After(time.Duration(2*expectedRunSec) * time.Second)
	done := make(chan bool)
	start := time.Now()
	var result *DiscoveryResult
	go func() {
		// Actual testing
		kubeCluster := repository.NewKubeCluster("MyCluster", []*v1.Node{})
		clusterSummary := repository.CreateClusterSummary(kubeCluster)
		dispatcher, resultCollector := getSamplingDispatcherAndCollector(workerCount, workerTimeout, taskRunTimeSec)
		var nodes = make([]struct{}, taskCount)
		go func() {
			for range nodes {
				node := new(v1.Node)
				currTask := task.NewTask().WithNode(node).WithCluster(clusterSummary)
				dispatcher.assignTask(currTask)
			}
		}()
		result = resultCollector.Collect(len(nodes))
		done <- true
	}()

	select {
	case <-testTimeout:
		t.Fatal("Test didn't finish in time")
	case <-done:
	}
	assert.Equal(t, taskCount, result.SuccessCount)
	assert.True(t, time.Since(start) > time.Duration(expectedRunSec)*time.Second)
}

func TestSamplingDispatcher_Dispatch_With_Task_Timeout(t *testing.T) {
	taskCount := 40
	workerCount := 20
	workerTimeout := 10
	taskRunTimeSec := 15 //longer than workerTimeout, the task will timeout and genearte a orphaned thread
	expectedRunSec := taskCount / workerCount * workerTimeout
	testTimeout := time.After(time.Duration(4*expectedRunSec) * time.Second)
	done := make(chan bool)
	var result *DiscoveryResult
	var threadCountAtBegin, threadCountAtEnd int = 0, 0
	threadCountAtBegin = runtime.NumGoroutine()
	go func() {
		// Actual testing
		dispatcher, resultCollector := getSamplingDispatcherAndCollector(workerCount, workerTimeout, taskRunTimeSec)
		var nodes = make([]struct{}, taskCount)
		go func() {
			for range nodes {
				currTask := task.NewTask()
				dispatcher.assignTask(currTask)
			}
		}()
		result = resultCollector.Collect(len(nodes))
		// Wait extra taskRunTimeSec to make sure all of the orphaned timeout thread finish their work
		time.Sleep(time.Duration(taskRunTimeSec) * time.Second)
		done <- true
	}()
	select {
	case <-testTimeout:
		t.Fatal("Test didn't finish in time")
	case <-done:
		threadCountAtEnd = runtime.NumGoroutine()
	}
	assert.Equal(t, taskCount, result.ErrorCount)
	// After all of orphaned thread is done, the total number of goroutine should be
	// less or equal to the number at the begining plus the workerCount
	assert.LessOrEqual(t, threadCountAtEnd, threadCountAtBegin+workerCount)
}
