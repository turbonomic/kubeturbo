package worker

import (
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"k8s.io/api/core/v1"
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

func (collector *GroupMetricsCollector) CollectGroupMetrics() ([]*repository.EntityGroup, error) {
	var entityGroupList []*repository.EntityGroup

	entityGroups := make(map[string]map[string]*repository.EntityGroup)
	entityGroupsByParentKind := make(map[string]*repository.EntityGroup)

	for _, pod := range collector.PodList {
		podKey := util.PodKeyFunc(pod)
		ownerTypeString, ownerString, err := collector.getGroupName(metrics.PodType, podKey)
		if err != nil {
			continue
		}

		podId := string(pod.UID)

		// Groups by parent type and instance
		ownerTypeMap, ownerTypeExists := entityGroups[ownerTypeString]
		if !ownerTypeExists {
			entityGroups[ownerTypeString] = make(map[string]*repository.EntityGroup)
			ownerTypeMap = entityGroups[ownerTypeString]
		}
		ownerTypeMap = entityGroups[ownerTypeString]
		if ownerString != "" {
			entityGroup, groupExists := ownerTypeMap[ownerTypeString]
			if !groupExists {
				// Create a new group for parent type & instance
				entityGroup, _ := repository.NewEntityGroup(ownerTypeString, ownerString)
				ownerTypeMap[ownerString] = entityGroup
				entityGroupList = append(entityGroupList, entityGroup)
			}

			entityGroup = entityGroups[ownerTypeString][ownerString]
			// Add pod member to the group
			entityGroup.AddMember(metrics.PodType, podId)

			for i := range pod.Spec.Containers {
				// Add container members to the group
				containerId := util.ContainerIdFunc(podId, i)
				entityGroup.AddMember(metrics.ContainerType, containerId)

				// Compute groups for different containers in the pod
				container := pod.Spec.Containers[i]
				containerList, containerGroupExists := entityGroup.ContainerGroups[container.Name]
				if !containerGroupExists {
					entityGroup.ContainerGroups[container.Name] = []string{}
				}
				containerList = append(containerList, containerId)
				entityGroup.ContainerGroups[container.Name] = append(entityGroup.ContainerGroups[container.Name], containerId)
			}
		}
		// Group by parent kind only
		entityGroupByParentKind, exists := entityGroupsByParentKind[ownerTypeString]
		if !exists {
			entityGroupByParentKind, _ := repository.NewEntityGroup(ownerTypeString, "")
			entityGroupsByParentKind[ownerTypeString] = entityGroupByParentKind
		}
		entityGroupByParentKind = entityGroupsByParentKind[ownerTypeString]
		entityGroupByParentKind.AddMember(metrics.PodType, podId)
	}

	return entityGroupList, nil
}

func (collector *GroupMetricsCollector) getGroupName(etype metrics.DiscoveredEntityType, entityKey string) (string, string, error) {
	ownerTypeMetricId := metrics.GenerateEntityStateMetricUID(etype, entityKey, metrics.OwnerType)
	ownerMetricId := metrics.GenerateEntityStateMetricUID(etype, entityKey, metrics.Owner)

	ownerTypeMetric, err := collector.MetricsSink.GetMetric(ownerTypeMetricId)
	if err != nil {
		return "", "", fmt.Errorf("Error getting owner type for pod %s --> %v\n", entityKey, err)
	}
	ownerType := ownerTypeMetric.GetValue()
	ownerTypeString, ok := ownerType.(string)
	if !ok || ownerTypeString == "" {
		return "", "", fmt.Errorf("Empty owner type for pod %s\n", entityKey)
	}

	ownerMetric, err := collector.MetricsSink.GetMetric(ownerMetricId)
	if err != nil {
		return "", "", fmt.Errorf("Error getting owner for pod %s --> %v\n", entityKey, err)
	}

	owner := ownerMetric.GetValue()
	ownerString, ok := owner.(string)
	if !ok || ownerString == "" {
		return "", "", fmt.Errorf("Empty owner for pod %s\n", entityKey)
	}

	return ownerTypeString, ownerString, nil
}
