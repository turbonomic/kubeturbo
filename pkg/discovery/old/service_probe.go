package old

import (
	"fmt"

	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

type ServiceProbe struct{}

func NewServiceProbe() *ServiceProbe {
	return &ServiceProbe{}
}

// Parse Services inside Kubernetes and build entityDTO as VApp.
func (svcProbe *ServiceProbe) ParseService(serviceList []*api.Service, endpointList []*api.Endpoints) (
	result []*proto.EntityDTO, err error) {
	err = nil
	// first make a endpoint map, key is endpoints cluster ID; value is endpoint object
	endpointMap := make(map[string]*api.Endpoints)
	for _, endpoint := range endpointList {
		nameWithNamespace := endpoint.Namespace + "/" + endpoint.Name
		endpointMap[nameWithNamespace] = endpoint
	}

	for _, service := range serviceList {
		serviceClusterID := service.Namespace + "/" + service.Name
		podClusterIDs := svcProbe.findPodEndpoints(service, endpointMap)
		if len(podClusterIDs) < 1 {
			glog.V(3).Infof("%s is a standalone service without any enpoint pod.", serviceClusterID)
			continue
		}
		glog.V(4).Infof("service %s has the following pod as endpoints %v", serviceClusterID, podClusterIDs)

		// create the commodities bought map.
		commoditiesBoughtMap, err := svcProbe.getCommoditiesBought(podClusterIDs)
		if err != nil {
			glog.Errorf("Failed to build commodities bought by service %s: %s", serviceClusterID, err)
			continue
		}

		// build the entityDTO.
		entityDto, err := svcProbe.buildServiceEntityDTO(service, commoditiesBoughtMap)
		if err != nil {
			glog.Errorf("Failed to build entityDTO for service %s: %s", serviceClusterID, err)
			continue
		}
		result = append(result, entityDto)
	}
	return
}

// For every service, find the pods serve this service.
func (svcProbe *ServiceProbe) findPodEndpoints(service *api.Service, endpointMap map[string]*api.Endpoints) []string {
	serviceNameWithNamespace := service.Namespace + "/" + service.Name
	serviceEndpoint := endpointMap[serviceNameWithNamespace]
	if serviceEndpoint == nil {
		return nil
	}
	subsets := serviceEndpoint.Subsets
	var podClusterIDList []string
	for _, endpointSubset := range subsets {
		addresses := endpointSubset.Addresses
		for _, address := range addresses {
			target := address.TargetRef
			if target == nil {
				continue
			}
			podName := target.Name
			podNamespace := target.Namespace
			podClusterID := GetPodClusterID(podNamespace, podName)
			// get the pod name and the service name
			podClusterIDList = append(podClusterIDList, podClusterID)
		}
	}
	return podClusterIDList
}

func (svcProbe *ServiceProbe) buildServiceEntityDTO(service *api.Service,
	commoditiesBoughtMap map[*builder.ProviderDTO][]*proto.CommodityDTO) (*proto.EntityDTO, error) {

	serviceEntityType := proto.EntityDTO_VIRTUAL_APPLICATION
	id := string(service.UID)
	displayName := "vApp-" + service.Namespace + "/" + service.Name + "-" + ClusterID
	entityDTOBuilder := builder.NewEntityDTOBuilder(serviceEntityType, id)

	entityDTOBuilder = entityDTOBuilder.DisplayName(displayName)
	for provider, commodities := range commoditiesBoughtMap {
		entityDTOBuilder.Provider(provider)
		entityDTOBuilder.BuysCommodities(commodities)
	}

	vAppData := &proto.EntityDTO_VirtualApplicationData{
		ServiceType: &service.Name,
	}
	entityDTOBuilder.VirtualApplicationData(vAppData)

	entityDto, err := entityDTOBuilder.Create()
	if err != nil {
		return nil, fmt.Errorf("Failed to build EntityDTO for service %s: %s",
			service.Namespace+"/"+service.Name, err)
	}

	glog.V(4).Infof("created a service entityDTO %v", entityDto)
	return entityDto, nil
}

func (svcProbe *ServiceProbe) getCommoditiesBought(podClusterIDList []string) (
	map[*builder.ProviderDTO][]*proto.CommodityDTO, error) {
	commoditiesBoughtMap := make(map[*builder.ProviderDTO][]*proto.CommodityDTO)

	for _, podClusterID := range podClusterIDList {
		serviceResourceStat := getServiceResourceStat(podTransactionCountMap, podClusterID)
		turboPodUUID, exist := turboPodUUIDMap[podClusterID]
		if !exist {
			return nil, fmt.Errorf("Cannot build commodityBought based on give pod identifier: %s. "+
				"Failed to find Turbo UUID.", podClusterID)
		}
		// Here it is consisted with the ID when we build the application entityDTO in ApplicationProbe/
		appID := appPrefix + turboPodUUID
		appType, exist := podAppTypeMap[podClusterID]
		if !exist {
			glog.V(3).Infof("No application has been discovered related to pod %s", podClusterID)
			continue
		}

		appProvider := builder.CreateProvider(proto.EntityDTO_APPLICATION, appID)
		var commoditiesBoughtFromApp []*proto.CommodityDTO
		transactionCommBought, err := builder.NewCommodityDTOBuilder(proto.CommodityDTO_TRANSACTION).
			Key(appType + "-" + ClusterID).
			Used(serviceResourceStat.transactionBought).
			Create()
		if err != nil {
			return nil, err
		}
		commoditiesBoughtFromApp = append(commoditiesBoughtFromApp, transactionCommBought)

		commoditiesBoughtMap[appProvider] = commoditiesBoughtFromApp
	}
	return commoditiesBoughtMap, nil
}

func getServiceResourceStat(transactionCountMap map[string]float64, podID string) *ServiceResourceStat {
	transactionBought := float64(0)

	count, ok := transactionCountMap[podID]
	if ok {
		transactionBought = count
		glog.V(4).Infof("Transaction bought from pod %s is %d", podID, count)
	} else {
		glog.V(4).Infof("No transaction value for applications on pod %s", podID)
	}

	return &ServiceResourceStat{
		transactionBought: transactionBought,
	}
}
