package worker

import (
	"fmt"

	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
)

// Collects parent info for the pods and containers and converts to EntityGroup objects
type GroupMetricsCollector struct {
	PodList     []*v1.Pod
	MetricsSink *metrics.EntityMetricSink
	workerId    string
}

func NewGroupMetricsCollector(discoveryWorker *k8sDiscoveryWorker, currTask *task.Task) *GroupMetricsCollector {
	metricsCollector := &GroupMetricsCollector{
		PodList:     currTask.PodList(),
		MetricsSink: discoveryWorker.sink,
		workerId:    discoveryWorker.id,
	}
	return metricsCollector
}

func (collector *GroupMetricsCollector) CollectGroupMetrics() []*repository.EntityGroup {
	var entityGroupList []*repository.EntityGroup

	entityGroupsByParentKind := make(map[string]*repository.EntityGroup)

	for _, pod := range collector.PodList {
		podKey := util.PodKeyFunc(pod)
		ownerTypeString, err := collector.getOwnerType(metrics.PodType, podKey)
		if err != nil {
			// TODO: Handle this informational logging in a better way
			// collector.getGroupName can return a bool in place of error
			// as its not really an error.
			glog.V(4).Infof(err.Error())
			continue
		}

		podId := string(pod.UID)
		// One global group by each parent kind/type
		if _, exists := entityGroupsByParentKind[ownerTypeString]; !exists {
			entityGroupsByParentKind[ownerTypeString] = repository.NewEntityGroup(ownerTypeString, ownerTypeString)
			entityGroupList = append(entityGroupList, entityGroupsByParentKind[ownerTypeString])
		}
		entityGroupByParentKind := entityGroupsByParentKind[ownerTypeString]

		// Add pod member to the group
		entityGroupByParentKind.AddMember(metrics.PodType, podId)
		for i := range pod.Spec.Containers {
			// Add container members to the group
			containerId := util.ContainerIdFunc(podId, i)
			entityGroupByParentKind.AddMember(metrics.ContainerType, containerId)
		}
	}

	return entityGroupList
}

func (collector *GroupMetricsCollector) getOwnerType(etype metrics.DiscoveredEntityType, entityKey string) (string, error) {
	ownerTypeMetricId := metrics.GenerateEntityStateMetricUID(etype, entityKey, metrics.OwnerType)

	ownerTypeMetric, err := collector.MetricsSink.GetMetric(ownerTypeMetricId)
	if err != nil {
		return "", fmt.Errorf("Error getting owner type for pod %s --> %v\n", entityKey, err)
	}
	ownerType := ownerTypeMetric.GetValue()
	ownerTypeString, ok := ownerType.(string)
	if !ok || ownerTypeString == "" {
		return "", fmt.Errorf("Empty owner type for pod %s\n", entityKey)
	}

	return ownerTypeString, nil
}
