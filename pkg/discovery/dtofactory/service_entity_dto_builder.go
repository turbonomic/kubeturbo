package dtofactory

import (
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const (
	vAppPrefix string = "vApp"
)

var (
	commodityTypeBetweenAppAndService map[proto.CommodityDTO_CommodityType]struct{} = map[proto.CommodityDTO_CommodityType]struct{}{
		proto.CommodityDTO_TRANSACTION: struct{}{},
	}
)

type ServiceEntityDTOBuilder struct{}

func (svcEntityDTOBuilder *ServiceEntityDTOBuilder) BuildSvcEntityDTO(servicePodMap map[*api.Service][]*api.Pod, clusterID string, appDTOs map[string]*proto.EntityDTO) ([]*proto.EntityDTO, error) {
	result := []*proto.EntityDTO{}
	for service, pods := range servicePodMap {
		serviceClusterID := util.GetServiceClusterID(service)
		serviceEntityType := proto.EntityDTO_VIRTUAL_APPLICATION
		id := string(service.UID)
		displayName := vAppPrefix + "-" + serviceClusterID + "-" + clusterID
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(serviceEntityType, id)

		entityDTOBuilder = entityDTOBuilder.DisplayName(displayName)

		// commodities bought.
		for _, pod := range pods {
			// Here it is consisted with the ID when we build the application entityDTO in ApplicationProbe
			appID := AppPrefix + string(pod.UID)
			appDTO, exist := appDTOs[appID]
			if !exist {
				glog.Errorf("Cannot find app %s in the application entityDTOs", appID)
			}
			appProvider := sdkbuilder.CreateProvider(proto.EntityDTO_APPLICATION, appID)
			commoditiesBoughtFromApp, err := svcEntityDTOBuilder.getCommoditiesBought(pod, appDTO)
			if err != nil {
				glog.Errorf("Failed to get commodity bought from %s.", appID)
				continue
			}
			entityDTOBuilder.Provider(appProvider)
			entityDTOBuilder.BuysCommodities(commoditiesBoughtFromApp)
		}

		// virtual application data.
		vAppData := &proto.EntityDTO_VirtualApplicationData{
			ServiceType: &service.Name,
		}
		entityDTOBuilder.VirtualApplicationData(vAppData)

		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build entityDTO for service %s: %s", serviceClusterID, err)
			continue
		}
		result = append(result, entityDto)
	}
	return result, nil
}

func (svcEntityDTOBuilder *ServiceEntityDTOBuilder) getCommoditiesBought(pod *api.Pod, appDTO *proto.EntityDTO) ([]*proto.CommodityDTO, error) {
	commoditiesSoldByApp := appDTO.GetCommoditiesSold()
	var commoditiesBoughtFromApp []*proto.CommodityDTO
	for _, commSold := range commoditiesSoldByApp {
		if _, exist := commodityTypeBetweenAppAndService[commSold.GetCommodityType()]; exist {
			commBoughtByService, err := sdkbuilder.NewCommodityDTOBuilder(commSold.GetCommodityType()).
				Key(commSold.GetKey()).
				Used(commSold.GetUsed()).
				Create()
			if err != nil {
				return nil, err
			}
			commoditiesBoughtFromApp = append(commoditiesBoughtFromApp, commBoughtByService)
		}
	}
	return commoditiesBoughtFromApp, nil
}
