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

type DiscoveryResult struct {
	EntityDTOs            []*proto.EntityDTO
	NamespaceMetrics      []*repository.NamespaceMetrics
	EntityGroups          []*repository.EntityGroup
	KubeControllers       []*repository.KubeController
	ContainerSpecMetrics  []*repository.ContainerSpecMetrics
	PodVolumeMetrics      []*repository.PodVolumeMetrics
	PodEntitiesMap        map[string]*repository.KubePod
	SidecarContainerSpecs []string
	PodsWithVolumes       []string
	NotReadyNodes         []string
	MirrorPodUids         []string
	SuccessCount          int
	ErrorCount            int
}

func NewResultCollector(maxWorkerNumber int) *ResultCollector {
	return &ResultCollector{
		resultPool: make(chan *task.TaskResult, maxWorkerNumber),
	}
}

func (rc *ResultCollector) ResultPool() chan *task.TaskResult {
	return rc.resultPool
}

func (rc *ResultCollector) Collect(count int) *DiscoveryResult {
	result := &DiscoveryResult{PodEntitiesMap: make(map[string]*repository.KubePod)}
	glog.V(2).Infof("Waiting for results from %d tasks.", count)

	stopChan := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(count)
	go func() {
		for {
			select {
			case <-stopChan:
				return
			case taskResult := <-rc.resultPool:
				if err := taskResult.Err(); err != nil {
					glog.Errorf("Discovery worker %s failed with error: %v", taskResult.WorkerId(), err)
					result.ErrorCount++
				} else {
					result.SuccessCount++
					glog.V(2).Infof("Processing results from worker %s", taskResult.WorkerId())
					// Entity DTOs for pods, nodes, containers from different workers
					result.EntityDTOs = append(result.EntityDTOs, taskResult.Content()...)
					// Namespace metrics from different workers
					result.NamespaceMetrics = append(result.NamespaceMetrics, taskResult.NamespaceMetrics()...)
					// Group data from different workers
					result.EntityGroups = append(result.EntityGroups, taskResult.EntityGroups()...)
					// Volume metrics from different workers
					result.PodVolumeMetrics = append(result.PodVolumeMetrics, taskResult.PodVolumeMetrics()...)
					// Pod data with apps from different workers
					for _, kubePod := range taskResult.PodEntities() {
						result.PodEntitiesMap[kubePod.PodClusterId] = kubePod
					}
					// K8s controller data from different workers
					result.KubeControllers = append(result.KubeControllers, taskResult.KubeControllers()...)
					// ContainerSpecs with individual container replica commodities data from different discovery workers
					result.ContainerSpecMetrics = append(result.ContainerSpecMetrics, taskResult.ContainerSpecMetrics()...)
					result.SidecarContainerSpecs = append(result.SidecarContainerSpecs, taskResult.SidecarContainerSpecs()...)
					result.PodsWithVolumes = append(result.PodsWithVolumes, taskResult.PodWithVolumes()...)
					result.MirrorPodUids = append(result.MirrorPodUids, taskResult.MirrorPodUids()...)
					result.NotReadyNodes = append(result.NotReadyNodes, taskResult.NotReadyNodes()...)
				}
				wg.Done()
			}
		}
	}()
	wg.Wait()
	// stop the result waiting goroutine.
	close(stopChan)
	glog.V(2).Infof("Got all the results from %d tasks with %d tasks failed and %d tasks succeeded.",
		count, result.ErrorCount, result.SuccessCount)
	return result
}
