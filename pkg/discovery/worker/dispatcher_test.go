package worker

import (
	"flag"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring"
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
	}
	dispatcherConfig := NewDispatcherConfig(clusterScraper, probeConfig, workerCount, 10)
	dispatcher := NewDispatcher(dispatcherConfig)
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
		_, _, _ = resultCollector.Collect(len(nodes))
		done <- true
	}()

	select {
	case <-timeout:
		t.Fatal("Test didn't finish in time")
	case <-done:
	}
}

// Test dispatching 4 tasks with 2 workers, each worker run for 60 second, expect the test to timeout in 20 seconds
func TestDispatcher_Dispatch_With_Task_Timeout(t *testing.T) {
	taskCount := 4
	workerCount := 2
	taskRunTimeSec := 60
	expectedRunSec := taskCount / workerCount * 10
	timeout := time.After(time.Duration(2*expectedRunSec) * time.Second)
	done := make(chan bool)
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
		_, _, _ = resultCollector.Collect(len(nodes))
		done <- true
	}()

	select {
	case <-timeout:
		t.Fatal("Test didn't finish in time")
	case <-done:
	}
}
