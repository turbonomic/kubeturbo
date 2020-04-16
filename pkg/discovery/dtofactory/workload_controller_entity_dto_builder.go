package dtofactory

import (
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/util"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type workloadControllerDTOBuilder struct {
	kubeControllersMap map[string]*repository.KubeController
	namespaceUIDMap    map[string]string
}

func NewWorkloadControllerDTOBuilder(kubeControllersMap map[string]*repository.KubeController,
	namespaceUIDMap map[string]string) *workloadControllerDTOBuilder {
	return &workloadControllerDTOBuilder{
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
		} else {
			glog.Errorf("Failed to get namespaceUID from namespace %s for controller %s", kubeController.Namespace,
				kubeController.GetFullName())
		}

		// Create WorkloadControllerData to store controller type data
		entityDTOBuilder.WorkloadControllerData(builder.createWorkloadControllerData(kubeController))

		// WorkloadController cannot be provisioned or suspended by Turbonomic analysis
		entityDTOBuilder.IsProvisionable(false)
		entityDTOBuilder.IsSuspendable(false)

		entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

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

func (builder *workloadControllerDTOBuilder) createWorkloadControllerData(kubeController *repository.KubeController) *proto.EntityDTO_WorkloadControllerData {
	controllerType := kubeController.ControllerType
	switch controllerType {
	case util.KindCronJob:
		return &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_CronJobData{
				CronJobData: &proto.EntityDTO_CronJobData{},
			},
		}
	case util.KindDaemonSet:
		return &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_DaemonSetData{
				DaemonSetData: &proto.EntityDTO_DaemonSetData{},
			},
		}
	case util.KindDeployment:
		return &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_DeploymentData{
				DeploymentData: &proto.EntityDTO_DeploymentData{},
			},
		}
	case util.KindJob:
		return &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_JobData{
				JobData: &proto.EntityDTO_JobData{},
			},
		}
	case util.KindReplicaSet:
		return &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_ReplicaSetData{
				ReplicaSetData: &proto.EntityDTO_ReplicaSetData{},
			},
		}
	case util.KindReplicationController:
		return &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_ReplicationControllerData{
				ReplicationControllerData: &proto.EntityDTO_ReplicationControllerData{},
			},
		}
	case util.KindStatefulSet:
		return &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_StatefulSetData{
				StatefulSetData: &proto.EntityDTO_StatefulSetData{},
			},
		}
	default:
		return &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_CustomControllerData{
				CustomControllerData: &proto.EntityDTO_CustomControllerData{
					CustomControllerType: &controllerType,
				},
			},
		}
	}
}
