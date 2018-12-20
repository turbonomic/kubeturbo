package worker

import (
	"strings"
	"sync"

	"github.com/turbonomic/kubeturbo/pkg/discovery/task"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"fmt"
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

func (rc *ResultCollector) Collect(count int) ([]*proto.EntityDTO, []*repository.QuotaMetrics, map[string]*repository.PolicyGroup) {
	discoveryResult := []*proto.EntityDTO{}
	quotaMetricsList := []*repository.QuotaMetrics{}
	policyGroupMap := make(map[string]*repository.PolicyGroup)
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
					// Combine members from different workers belonging to the same group
					for groupName, policyGroup := range result.PolicyGroups() {
						existingGroup, exists := policyGroupMap[groupName]
						if exists {
							//fmt.Printf("[resultCollector] %s - Adding members to group: %s\n", result.WorkerId(), groupName)
							existingGroup.Members = append(existingGroup.Members, policyGroup.Members...)
						} else {
							//fmt.Printf("[resultCollector] %s - Creating new group: %s\n", result.WorkerId(), groupName)
							policyGroupMap[groupName] = policyGroup
						}
					}
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

	return discoveryResult, quotaMetricsList, policyGroupMap
}
