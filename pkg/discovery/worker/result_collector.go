package worker

import (
	"sync"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type ResultCollector struct {
	// If the number of workers larger than maxWorkerNumber, then the discovery result of extra workers will be block.
	resultPool chan *task.TaskResult
}

func NewResultCollector(maxWorkerNumber int) *ResultCollector {
	return &ResultCollector{
		resultPool: make(chan *task.TaskResult, maxWorkerNumber),
	}
}

func (rc *ResultCollector) ResultPool() chan *task.TaskResult {
	return rc.resultPool
}

func (rc *ResultCollector) Collect(count int) ([]*proto.EntityDTO, []*repository.QuotaMetrics, []*repository.EntityGroup) {
	//map[string]*repository.PolicyGroup) {
	var discoveryResult []*proto.EntityDTO
	var quotaMetricsList []*repository.QuotaMetrics
	var entityGroupList []*repository.EntityGroup

	successCount := 0
	errorCount := 0

	glog.V(2).Infof("Waiting for results from %d tasks.", count)

	stopChan := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(count)
	go func() {
		for {
			select {
			case <-stopChan:
				return
			case result := <-rc.resultPool:
				if err := result.Err(); err != nil {
					glog.Errorf("Discovery worker %s failed with error: %v", result.WorkerId(), err)
					errorCount++
				} else {
					successCount++
					glog.V(2).Infof("Processing results from worker %s", result.WorkerId())
					// Entity DTOs for pods, nodes, containers from different workers
					discoveryResult = append(discoveryResult, result.Content()...)
					// Quota metrics from different workers
					quotaMetricsList = append(quotaMetricsList, result.QuotaMetrics()...)
					// Group data from different workers
					entityGroupList = append(entityGroupList, result.EntityGroups()...)
				}
				wg.Done()
			}
		}
	}()
	wg.Wait()
	// stop the result waiting goroutine.
	close(stopChan)
	glog.V(2).Infof("Got all the results from %d tasks with %d tasks failed and %d tasks succeeded.",
		count, errorCount, successCount)
	return discoveryResult, quotaMetricsList, entityGroupList
}
