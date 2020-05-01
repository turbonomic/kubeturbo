package dtofactory

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	api "k8s.io/api/core/v1"

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
	commodityTypeBetweenAppAndService = map[proto.CommodityDTO_CommodityType]struct{}{
		proto.CommodityDTO_APPLICATION: {},
	}
)

type ServiceEntityDTOBuilder struct {
	// Services to list of pods
	Services map[*api.Service][]string
	// Pods with app DTOs
	PodEntitiesMap map[string]*repository.KubePod
}

func NewServiceEntityDTOBuilder(services map[*api.Service][]string,
	podEntitiesMap map[string]*repository.KubePod) *ServiceEntityDTOBuilder {

	builder := &ServiceEntityDTOBuilder{
		Services:       services,
		PodEntitiesMap: podEntitiesMap,
	}

	return builder
}

func (builder *ServiceEntityDTOBuilder) BuildDTOs() ([]*proto.EntityDTO, error) {
	result := []*proto.EntityDTO{}

	for service, podList := range builder.Services {
		serviceName := util.GetServiceClusterID(service)
		// collection of pods and apps for this service
		var pods []*api.Pod
		appEntityDTOsMap := make(map[string]*proto.EntityDTO)

		if len(podList) == 0 {
			glog.Errorf("Service %s has no pods", serviceName)
			continue
		}

		for _, podClusterId := range podList {
			glog.V(4).Infof("service %s --> pod %s", service.Name, podClusterId)
			kubePod := builder.PodEntitiesMap[podClusterId]
			if kubePod == nil {
				glog.Errorf("Missing pod for pod id : %s", podClusterId)
				continue
			}
			pods = append(pods, kubePod.Pod)
			for _, appDto := range kubePod.ContainerApps {
				appEntityDTOsMap[appDto.GetId()] = appDto
			}
		}

		id := string(service.UID)
		displayName := fmt.Sprintf("%s-%s", vAppPrefix, serviceName)

		ebuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_SERVICE, id).
			DisplayName(displayName)

		//1. commodities bought
		if err := builder.createCommodityBought(ebuilder, pods, appEntityDTOsMap); err != nil {
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

		ebuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

		//3. create it
		entityDto, err := ebuilder.Create()
		if err != nil {
			glog.Errorf("failed to create service[%s] EntityDTO: %v", displayName, err)
			continue
		}
		glog.V(4).Infof("service DTO: %++v", entityDto)
		result = append(result, entityDto)
	}

	return result, nil
}

func (builder *ServiceEntityDTOBuilder) createCommodityBought(ebuilder *sdkbuilder.EntityDTOBuilder,
	pods []*api.Pod, appDTOs map[string]*proto.EntityDTO) error {
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
			provider := sdkbuilder.CreateProvider(proto.EntityDTO_APPLICATION_COMPONENT, appId)
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

func (builder *ServiceEntityDTOBuilder) getCommoditiesBought(appDTO *proto.EntityDTO) ([]*proto.CommodityDTO, error) {
	commoditiesSoldByApp := appDTO.GetCommoditiesSold()
	var commoditiesBoughtFromApp []*proto.CommodityDTO
	for _, commSold := range commoditiesSoldByApp {
		if _, exist := commodityTypeBetweenAppAndService[commSold.GetCommodityType()]; exist {
			commBuilder := sdkbuilder.NewCommodityDTOBuilder(commSold.GetCommodityType()).
				Key(commSold.GetKey())
			if commSold.Used != nil {
				commBuilder.Used(commSold.GetUsed())
			}
			commBoughtByService, err := commBuilder.Create()
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
