package dtofactory

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
)

type containerSpecDTOBuilder struct {
	generalBuilder
}

func NewContainerSpecDTOBuilder(sink *metrics.EntityMetricSink) *containerSpecDTOBuilder {
	return &containerSpecDTOBuilder{
		generalBuilder: newGeneralBuilder(sink),
	}
}

// TODO consider passing []*repository.KubeContainer as argument which stores commodities data of each container instance
func (builder *containerSpecDTOBuilder) BuildDTOs(pods []*api.Pod) ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	controllerUIDToContainersMap := builder.getControllerUIDToContainersMap(pods)
	for controllerUID, containersSet := range controllerUIDToContainersMap {
		for containerName := range containersSet {
			containerSpecID := util.ContainerSpecIdFunc(controllerUID, containerName)
			entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_CONTAINER_SPEC, containerSpecID)
			entityDTOBuilder.DisplayName(containerName)

			// Make ContainerSpec entity not monitored
			entityDTOBuilder.Monitored(false)
			dto, err := entityDTOBuilder.Create()
			if err != nil {
				glog.Errorf("failed to build ContainerSpec[%s] entityDTO: %v", containerName, err)
			}
			result = append(result, dto)
		}
	}
	return result, nil
}

// Get a map from controller ID to set of container names.
func (builder *containerSpecDTOBuilder) getControllerUIDToContainersMap(pods []*api.Pod) map[string]map[string]struct{} {
	controllerUIDToContainersMap := make(map[string]map[string]struct{})
	for _, pod := range pods {
		if pod.OwnerReferences == nil {
			// If pod has no OwnerReferences, it is a bare pod without controller. Skip this.
			continue
		}
		controllerUID, err := builder.getControllerUID(metrics.PodType, util.PodKeyFunc(pod))
		if err != nil {
			continue
		}
		containerSet, exist := controllerUIDToContainersMap[controllerUID]
		if !exist {
			containerSet = make(map[string]struct{})
		}
		for _, container := range pod.Spec.Containers {
			containerSet[container.Name] = struct{}{}
		}
		controllerUIDToContainersMap[controllerUID] = containerSet
	}
	return controllerUIDToContainersMap
}

func (builder *containerSpecDTOBuilder) getControllerUID(entityType metrics.DiscoveredEntityType, entityKey string) (string, error) {
	ownerUIDMetricId := metrics.GenerateEntityStateMetricUID(entityType, entityKey, metrics.OwnerUID)
	ownerUIDMetric, err := builder.metricsSink.GetMetric(ownerUIDMetricId)
	if err != nil {
		return "", fmt.Errorf("Error getting owner UID for pod %s --> %v\n", entityKey, err)
	}
	ownerUID := ownerUIDMetric.GetValue()
	controllerUID, ok := ownerUID.(string)
	if !ok {
		return "", fmt.Errorf("Error getting owner UID for pod %s\n", entityKey)
	}
	return controllerUID, nil
}
