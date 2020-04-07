package worker

import (
	"strings"
	"sync"

	"github.com/turbonomic/kubeturbo/pkg/discovery/task"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

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

func (rc *ResultCollector) Collect(count int) ([]*proto.EntityDTO, map[string]*repository.KubePod,
	[]*repository.NamespaceMetrics, []*repository.EntityGroup) {
	discoveryResult := []*proto.EntityDTO{}
	namespaceMetrics := []*repository.NamespaceMetrics{}
	entityGroupList := []*repository.EntityGroup{}
	discoveryErrorString := []string{}
	podEntitiesMap := make(map[string]*repository.KubePod)
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
				glog.V(2).Infof("Processing results from worker %s", result.WorkerId())
				if err := result.Err(); err != nil {
					discoveryErrorString = append(discoveryErrorString, err.Error())
				} else {
					// Entity DTOs for pods, nodes, containers from different workers
					discoveryResult = append(discoveryResult, result.Content()...)
					// Namespace metrics from different workers
					namespaceMetrics = append(namespaceMetrics, result.NamespaceMetrics()...)
					// Group data from different workers
					entityGroupList = append(entityGroupList, result.EntityGroups()...)
					// Pod data with apps from different workers
					for _, kubePod := range result.PodEntities() {
						podEntitiesMap[kubePod.PodClusterId] = kubePod
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

	return discoveryResult, podEntitiesMap, namespaceMetrics, entityGroupList
}
