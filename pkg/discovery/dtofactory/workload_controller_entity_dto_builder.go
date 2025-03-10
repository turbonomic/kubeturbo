package dtofactory

import (
	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/detectors"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	discoveryUtil "github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
	sdkbuilder "github.ibm.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	v1 "k8s.io/api/core/v1"
	"strings"
)

type workloadControllerDTOBuilder struct {
	clusterSummary     *repository.ClusterSummary
	kubeControllersMap map[string]*repository.KubeController
	namespaceUIDMap    map[string]string
}

func NewWorkloadControllerDTOBuilder(clusterSummary *repository.ClusterSummary, kubeControllersMap map[string]*repository.KubeController,
	namespaceUIDMap map[string]string) *workloadControllerDTOBuilder {
	return &workloadControllerDTOBuilder{
		clusterSummary:     clusterSummary,
		kubeControllersMap: kubeControllersMap,
		namespaceUIDMap:    namespaceUIDMap,
	}
}

// Build entityDTOs based on the given map from controller UID to KubeController entity.
func (builder *workloadControllerDTOBuilder) BuildDTOs() ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	for _, kubeController := range builder.kubeControllersMap {
		// Id
		workloadControllerId := kubeController.UID
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_WORKLOAD_CONTROLLER, workloadControllerId)
		// Display name
		workloadControllerDisplayName := kubeController.Name
		entityDTOBuilder.DisplayName(workloadControllerDisplayName)

		// Resource commodities sold
		commoditiesSold, err := builder.getCommoditiesSold(kubeController)
		if err != nil {
			glog.Errorf("Error creating commoditiesSold for %s: %v", kubeController.GetFullName(), err)
			continue
		}
		entityDTOBuilder.SellsCommodities(commoditiesSold)

		// Resource commodities bought from namespace
		namespaceUID, exists := builder.namespaceUIDMap[kubeController.Namespace]
		if exists {
			commoditiesBought, err := builder.getCommoditiesBought(kubeController, namespaceUID)
			if err != nil {
				glog.Errorf("Error creating commoditiesBought for %s: %v", kubeController.GetFullName(), err)
				continue
			}
			entityDTOBuilder.Provider(sdkbuilder.CreateProvider(proto.EntityDTO_NAMESPACE, namespaceUID)).BuysCommodities(commoditiesBought)
			// Set movable false to avoid moving WorkloadController across namespaces
			entityDTOBuilder.IsMovable(proto.EntityDTO_NAMESPACE, false)
			// also set up the aggregatedBy relationship with the namespace
			entityDTOBuilder.AggregatedBy(namespaceUID)
		} else {
			glog.Errorf("Failed to get namespaceUID from namespace %s for controller %s", kubeController.Namespace,
				kubeController.GetFullName())
		}

		// Connect WorkloadController to ContainerSpec entity, WorkloadController owns the associated ContainerSpecs.
		containerSpecsIds := builder.getContainerSpecIds(kubeController)
		for _, containerSpecId := range containerSpecsIds {
			entityDTOBuilder.Owns(containerSpecId)
		}

		// WorkloadController cannot be provisioned or suspended by Turbonomic analysis
		entityDTOBuilder.IsProvisionable(false)
		entityDTOBuilder.IsSuspendable(false)

		entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)
		fqn := strings.Join([]string{builder.clusterSummary.Name, kubeController.Namespace, kubeController.Name},
			util.NamingQualifierSeparator)
		entityDTOBuilder.WithProperty(property.BuildNamespaceProperty(kubeController.Namespace)).
			WithProperty(property.BuildWorkloadControllerNameProperty(kubeController.Name)).
			WithProperty(property.BuildFullyQualifiedNameProperty(fqn))

		var replicas int32
		if builder.clusterSummary != nil {
			controller, found := builder.clusterSummary.ControllerMap[workloadControllerId]
			if found {
				entityDTOBuilder.WithProperties(property.BuildLabelAnnotationProperties(controller.Labels, controller.Annotations, detectors.AWWorkloadController))
				if controller.Replicas != nil {
					replicas = int32(*controller.Replicas)
				}
			}
		}

		// Create WorkloadControllerData to store controller type data
		entityDTOBuilder.WorkloadControllerData(builder.createWorkloadControllerData(kubeController, replicas))

		entityDTO, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("failed to build WorkloadController[%s] entityDTO: %v", workloadControllerDisplayName, err)
		}

		result = append(result, entityDTO)
	}
	return result, nil
}

func (builder *workloadControllerDTOBuilder) getCommoditiesSold(kubeController *repository.KubeController) ([]*proto.CommodityDTO, error) {
	var commoditiesSold []*proto.CommodityDTO
	for resourceType, resource := range kubeController.AllocationResources {
		commodityType, exist := rTypeMapping[resourceType]
		if !exist {
			glog.Errorf("ResourceType %s is not supported", resourceType)
			continue
		}
		commSoldBuilder := sdkbuilder.NewCommodityDTOBuilder(commodityType)
		commSoldBuilder.Used(resource.Used)
		commSoldBuilder.Peak(resource.Used)
		commSoldBuilder.Capacity(resource.Capacity)
		commSoldBuilder.Resizable(false)
		commSoldBuilder.Key(kubeController.UID)
		commSold, err := commSoldBuilder.Create()
		if err != nil {
			glog.Errorf("%s: Failed to build commodity sold %s: %s", kubeController.GetFullName(), commodityType, err)
			continue
		}
		commoditiesSold = append(commoditiesSold, commSold)
	}
	return commoditiesSold, nil
}

func (builder *workloadControllerDTOBuilder) getCommoditiesBought(kubeController *repository.KubeController,
	namespaceUID string) ([]*proto.CommodityDTO, error) {
	var commoditiesBought []*proto.CommodityDTO
	for resourceType, resource := range kubeController.AllocationResources {
		commodityType, exist := rTypeMapping[resourceType]
		if !exist {
			glog.Errorf("ResourceType %s is not supported", resourceType)
			continue
		}
		commBoughtBuilder := sdkbuilder.NewCommodityDTOBuilder(commodityType)
		commBoughtBuilder.Used(resource.Used)
		commBoughtBuilder.Peak(resource.Used)
		commBoughtBuilder.Key(namespaceUID)
		commBought, err := commBoughtBuilder.Create()
		if err != nil {
			glog.Errorf("%s: Failed to build commodity bought %s: %s", kubeController.GetFullName(), commodityType, err)
			continue
		}
		commoditiesBought = append(commoditiesBought, commBought)
	}
	return commoditiesBought, nil
}

// Get a slice of containerSpec id from the given KubeController entity
func (builder *workloadControllerDTOBuilder) getContainerSpecIds(kubeController *repository.KubeController) []string {
	containerNameSet := make(map[string]struct{})
	for _, pod := range kubeController.Pods {
		// Pods scheduled on a node, but still in Pending phase do not have containers started, exclude them
		// when getting container specs
		if discoveryUtil.PodIsPending(pod) {
			glog.V(3).Infof("Skipping pod %v when building containerSpecs owned by controller %v."+
				" There is no container started in the pod.", discoveryUtil.GetPodClusterID(pod),
				kubeController.GetFullName())
			continue
		}
		for _, container := range pod.Spec.Containers {
			containerNameSet[container.Name] = struct{}{}
		}
	}
	var containerSpecIds []string
	for containerName := range containerNameSet {
		containerSpecId := discoveryUtil.ContainerSpecIdFunc(kubeController.UID, containerName)
		containerSpecIds = append(containerSpecIds, containerSpecId)
	}
	return containerSpecIds
}

func getActiveReplicaCount(kubeController *repository.KubeController) int32 {
	replicaCount := 0
	for _, pod := range kubeController.Pods {
		if pod.Status.Phase == v1.PodRunning {
			replicaCount++
		}
	}
	return int32(replicaCount)
}

// CreateWorkloadControllerDataByControllerType creates and returns a *proto.EntityDTO_WorkloadControllerData
// based on the provided controller kind.
//
// The supported controller kinds are:
//   - KindCronJob
//   - KindDaemonSet
//   - KindDeployment
//   - KindJob
//   - KindReplicaSet
//   - KindReplicationController
//   - KindStatefulSet
//
// If the provided kind does not match any known controller type, it returns CustomControllerData with its type specified.
func CreateWorkloadControllerDataByControllerType(kind string) *proto.EntityDTO_WorkloadControllerData {
	var data *proto.EntityDTO_WorkloadControllerData

	switch kind {
	case util.KindCronJob:
		data = &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_CronJobData{
				CronJobData: &proto.EntityDTO_CronJobData{},
			},
		}
	case util.KindDaemonSet:
		data = &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_DaemonSetData{
				DaemonSetData: &proto.EntityDTO_DaemonSetData{},
			},
		}
	case util.KindDeployment:
		data = &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_DeploymentData{
				DeploymentData: &proto.EntityDTO_DeploymentData{},
			},
		}
	case util.KindJob:
		data = &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_JobData{
				JobData: &proto.EntityDTO_JobData{},
			},
		}
	case util.KindReplicaSet:
		data = &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_ReplicaSetData{
				ReplicaSetData: &proto.EntityDTO_ReplicaSetData{},
			},
		}
	case util.KindReplicationController:
		data = &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_ReplicationControllerData{
				ReplicationControllerData: &proto.EntityDTO_ReplicationControllerData{},
			},
		}
	case util.KindStatefulSet:
		data = &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_StatefulSetData{
				StatefulSetData: &proto.EntityDTO_StatefulSetData{},
			},
		}
	default:
		data = &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_CustomControllerData{
				CustomControllerData: &proto.EntityDTO_CustomControllerData{
					CustomControllerType: &kind,
				},
			},
		}
	}

	return data
}

func (builder *workloadControllerDTOBuilder) createWorkloadControllerData(kubeController *repository.KubeController, replicas int32) *proto.EntityDTO_WorkloadControllerData {
	controllerType := kubeController.ControllerType
	data := CreateWorkloadControllerDataByControllerType(controllerType)

	if data != nil {
		data.ReplicaCount = &replicas
	}

	return data
}
