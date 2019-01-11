package worker

import (
	"strings"
	"sync"

	"github.com/turbonomic/kubeturbo/pkg/discovery/task"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
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
	discoveryResult := []*proto.EntityDTO{}
	quotaMetricsList := []*repository.QuotaMetrics{}
	entityGroupList := []*repository.EntityGroup{}
	discoveryErrorString := []string{}

	glog.V(2).Infof("Waiting for results from %d workers.", count)

	stopChan := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(count)
	go func() {
		for {
			select {
			case <-stopChan:
				return
			case result := <-rc.resultPool:
				fmt.Printf("[resultCollector] Processing results from %s\n", result.WorkerId())
				if err := result.Err(); err != nil {
					discoveryErrorString = append(discoveryErrorString, err.Error())
				} else {
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
	glog.V(2).Infof("Got all the results from %d workers.", count)

	if len(discoveryErrorString) > 0 {
		glog.Errorf("One or more discovery worker failed: %s", strings.Join(discoveryErrorString, "\t\t"))
	}

	return discoveryResult, quotaMetricsList, entityGroupList
}
