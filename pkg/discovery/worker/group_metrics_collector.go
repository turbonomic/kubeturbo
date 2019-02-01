package worker

import (
	"fmt"
	"github.com/golang/glog"
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
	etype := metrics.PodType

	podMembers := make(map[string]map[string][]string)       // pods by parent kind and instance
	containerMembers := make(map[string]map[string][]string) // container by parent kind and instance
	podByParentMembers := make(map[string][]string)          // pods by parent kind

	// Iterate over list of pods to get the owner metric for each
	for _, pod := range collector.PodList {
		// Parent for the pod
		podKey := util.PodKeyFunc(pod)
		ownerTypeString, ownerString, err := collector.getGroupName(etype, podKey)
		if err != nil {
			continue
		}

		// Add pod id to its owner group membership
		podId := string(pod.UID)
		ownerTypeMap, ownerTypeExists := podMembers[ownerTypeString]
		if !ownerTypeExists {
			podMembers[ownerTypeString] = make(map[string][]string)
			ownerTypeMap = podMembers[ownerTypeString]
		}
		ownerTypeMap[ownerString] = append(ownerTypeMap[ownerString], podId)

		podByParentMembers[ownerTypeString] = append(podByParentMembers[ownerTypeString], podId)

		// Container group membership - same as pod group
		podMId := util.PodMetricIdAPI(pod)
		for i := range pod.Spec.Containers { //
			container := &(pod.Spec.Containers[i])
			containerMId := util.ContainerMetricId(podMId, container.Name)

			cOwnerTypeMap, cOwnerTypeExists := containerMembers[ownerTypeString]
			if !cOwnerTypeExists {
				containerMembers[ownerTypeString] = make(map[string][]string)
				cOwnerTypeMap = containerMembers[ownerTypeString]
			}
			cOwnerTypeMap[ownerString] = append(cOwnerTypeMap[ownerString], containerMId)
		}
	}

	// Pod and container group per parent type and instance
	var entityGroupList []*repository.EntityGroup
	for ownerType, ownerTypeMap := range podMembers {
		for ownerInstance, podList := range ownerTypeMap {
			entityGroup, err := repository.NewEntityGroup(ownerType, ownerInstance)
			if err != nil {
				glog.Errorf("%v", err)
				continue
			}
			for _, pod := range podList {
				entityGroup.AddMember(metrics.PodType, pod)
			}
			containerList, exists := containerMembers[ownerType][ownerInstance]
			if exists {
				for _, container := range containerList {
					entityGroup.AddMember(metrics.ContainerType, container)
				}
			}
			entityGroupList = append(entityGroupList, entityGroup)
			glog.V(4).Infof("[discovery_worker] created group --> %++v\n", entityGroup.GroupId)
		}
	}

	// Pod Group per parent type
	for ownerType, podList := range podByParentMembers {
		entityGroup, _ := repository.NewEntityGroup(ownerType, "")
		for _, pod := range podList {
			entityGroup.AddMember(metrics.PodType, pod)
		}
		entityGroupList = append(entityGroupList, entityGroup)
		glog.V(4).Infof("[discovery_worker] created group --> %++v\n", entityGroup.GroupId)
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
