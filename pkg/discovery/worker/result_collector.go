package worker

import (
	"strings"
	"sync"

	"github.com/turbonomic/kubeturbo/pkg/discovery/task"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
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

func (rc *ResultCollector) Collect(count int) []*proto.EntityDTO {
	discoveryResult := []*proto.EntityDTO{}
	discoveryErrorString := []string{}

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
					discoveryErrorString = append(discoveryErrorString, err.Error())
				} else {
					discoveryResult = append(discoveryResult, result.Content()...)
				}
				wg.Done()
			}
		}
	}()
	wg.Wait()
	// stop the result waiting goroutine.
	stopChan <- struct{}{}

	if len(discoveryErrorString) > 0 {
		glog.Errorf("One or more discovery worker failed: %s", strings.Join(discoveryErrorString, "\t\t"))
	}

	return discoveryResult
}
