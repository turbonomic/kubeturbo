package dtofactory

import (
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"strings"
)

const (
	vAppPrefix string = "vApp"
)

var (
	commodityTypeBetweenAppAndService map[proto.CommodityDTO_CommodityType]struct{} = map[proto.CommodityDTO_CommodityType]struct{}{
		proto.CommodityDTO_TRANSACTION:   struct{}{},
		proto.CommodityDTO_RESPONSE_TIME: struct{}{},
	}
)

type ServiceEntityDTOBuilder struct{}

func (builder *ServiceEntityDTOBuilder) BuildSvcEntityDTO(servicePodMap map[*api.Service][]*api.Pod, clusterID string, appDTOs map[string]*proto.EntityDTO) ([]*proto.EntityDTO, error) {
	result := []*proto.EntityDTO{}

	for service, pods := range servicePodMap {
		id := string(service.UID)
		serviceName := util.GetServiceClusterID(service)
		displayName := fmt.Sprintf("%s-%s", vAppPrefix, serviceName)

		ebuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_VIRTUAL_APPLICATION, id).
			DisplayName(displayName)

		//1. commodities bought
		if err := builder.createCommodityBought(ebuilder, pods, appDTOs); err != nil {
			glog.Errorf("failed to create server[%s] EntityDTO: %v", serviceName, err)
			continue
		}

		//2. virtual application data.
		vAppData := &proto.EntityDTO_VirtualApplicationData{
			ServiceType: &service.Name,
		}
		ebuilder.VirtualApplicationData(vAppData)

		// set the ip property for stitching
		ebuilder.WithProperty(getIPProperty(pods))

		//3. check whether it is monitored
		if !util.IsMonitoredFromAnnotation(service.GetAnnotations()) {
			glog.V(3).Infof("Service %v is not monitored.", displayName)
			ebuilder.Monitored(false)
		}

		ebuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

		//4. create it
		entityDto, err := ebuilder.Create()
		if err != nil {
			glog.Errorf("failed to create service[%s] EntityDTO: %v", displayName, err)
			continue
		}
		result = append(result, entityDto)
	}

	return result, nil
}

func (builder *ServiceEntityDTOBuilder) createCommodityBought(ebuilder *sdkbuilder.EntityDTOBuilder, pods []*api.Pod, appDTOs map[string]*proto.EntityDTO) error {
	foundProvider := false
	for _, pod := range pods {
		podId := string(pod.UID)
		for i := range pod.Spec.Containers {
			containerName := util.ContainerNameFunc(pod, &(pod.Spec.Containers[i]))
			containerId := util.ContainerIdFunc(podId, i)
			appId := util.ApplicationIdFunc(containerId)

			appDTO, exist := appDTOs[appId]
			if !exist {
				glog.Errorf("Cannot find app[%s] entityDTO of container[%s].", appId, containerName)
				continue
			}

			bought, err := builder.getCommoditiesBought(appDTO)
			if err != nil {
				glog.Errorf("failed to get commodity bought from container[%s]: %v", containerName, err)
				continue
			}
			provider := sdkbuilder.CreateProvider(proto.EntityDTO_APPLICATION, appId)
			ebuilder.Provider(provider).BuysCommodities(bought)
			foundProvider = true
		}
	}

	if !foundProvider {
		err := fmt.Errorf("Failed to found any provider for service")
		glog.Warning(err.Error())
		return err
	}

	return nil
}

func (svcEntityDTOBuilder *ServiceEntityDTOBuilder) getCommoditiesBought(appDTO *proto.EntityDTO) ([]*proto.CommodityDTO, error) {
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

	if len(commoditiesBoughtFromApp) < 1 {
		return nil, fmt.Errorf("no commodity found.")
	}

	return commoditiesBoughtFromApp, nil
}

// Get the IP property of the vApp for stitching purpose
func getIPProperty(pods []*api.Pod) *proto.EntityDTO_EntityProperty {
	ns := stitching.DefaultPropertyNamespace
	attr := stitching.AppStitchingAttr
	ips := []string{}
	for _, pod := range pods {
		ips = append(ips, vAppPrefix+"-"+pod.Status.PodIP)
	}
	ip := strings.Join(ips, ",")
	ipProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &ns,
		Name:      &attr,
		Value:     &ip,
	}

	return ipProperty
}
