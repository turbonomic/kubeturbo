package dtofactory

import (
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	api "k8s.io/api/core/v1"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"fmt"
	"strings"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
)

const (
	servicePrefix string = "Service"
)

var (
	commodityTypeBetweenAppAndService = map[proto.CommodityDTO_CommodityType]struct{}{
		proto.CommodityDTO_APPLICATION: {},
	}
)

type ServiceEntityDTOBuilder struct {
	ClusterSummary *repository.ClusterSummary
	ClusterScraper *cluster.ClusterScraper
	// Pods with app DTOs
	PodEntitiesMap map[string]*repository.KubePod
}

func NewServiceEntityDTOBuilder(clusterSummary *repository.ClusterSummary,
	clusterScraper *cluster.ClusterScraper, podEntitiesMap map[string]*repository.KubePod) *ServiceEntityDTOBuilder {
	builder := &ServiceEntityDTOBuilder{
		ClusterSummary: clusterSummary,
		PodEntitiesMap: podEntitiesMap,
		ClusterScraper: clusterScraper,
	}
	return builder
}

func (builder *ServiceEntityDTOBuilder) BuildDTOs() []*proto.EntityDTO {
	var result []*proto.EntityDTO
	svcID, err := builder.ClusterScraper.GetKubernetesServiceID()
	if err != nil {
		glog.Warningf("Failed to get Kubernetes service ID: %v", err)
	}
	for service, podList := range builder.ClusterSummary.Services {
		serviceName := util.GetServiceClusterID(service)
		if len(podList) == 0 {
			glog.Warningf("Service %s has no pods", serviceName)
			continue
		}
		// collection of pods and apps for this service
		var pods []*api.Pod
		appEntityDTOsMap := make(map[string]*proto.EntityDTO)
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
		displayName := fmt.Sprintf("%s-%s", servicePrefix, serviceName)

		ebuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_SERVICE, id).
			DisplayName(displayName)

		// commodities sold
		if err := builder.createCommoditySold(ebuilder, pods, serviceName); err != nil {
			glog.Warningf("Failed to create commodity sold for service %s: %v", serviceName, err)
			continue
		}

		// commodities bought
		if err := builder.createCommodityBought(ebuilder, pods, appEntityDTOsMap); err != nil {
			glog.Errorf("Failed to create server[%s] EntityDTO: %v", serviceName, err)
			continue
		}

		// service data.
		ebuilder.ServiceData(createServiceData(service))

		// set the ip property for stitching
		ebuilder.WithProperty(getIPProperty(pods, svcID)).WithProperty(getUUIDProperty(id))

		ebuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

		//3. create it
		entityDto, err := ebuilder.Create()
		if err != nil {
			glog.Errorf("Failed to create service[%s] EntityDTO: %v", displayName, err)
			continue
		}
		glog.V(4).Infof("service DTO: %++v", entityDto)
		result = append(result, entityDto)
	}

	return result
}

func createServiceData(service *api.Service) *proto.EntityDTO_ServiceData {
	serviceData := &proto.EntityDTO_ServiceData{
		IpAddress: &service.Spec.ClusterIP,
	}
	k8sServiceType := string(service.Spec.Type)
	if value, ok := proto.EntityDTO_KubernetesServiceData_ServiceType_value[k8sServiceType]; ok {
		serviceType := proto.EntityDTO_KubernetesServiceData_ServiceType(value)
		serviceData.ServiceData = &proto.EntityDTO_ServiceData_KubernetesServiceData{
			KubernetesServiceData: &proto.EntityDTO_KubernetesServiceData{
				ServiceType: &serviceType,
			},
		}
	}
	return serviceData
}

// Create sold commodities for Service entity.
// Currently, only NumberReplicas is sold by Service
func (builder *ServiceEntityDTOBuilder) createCommoditySold(
	ebuilder *sdkbuilder.EntityDTOBuilder, pods []*api.Pod, serviceName string) error {
	var controllers = make(map[string]*repository.K8sController)
	readyPods := 0
	for _, pod := range pods {
		// Get the pod controller info from the podToController cache in ClusterScraper
		ownerInfo, _, _, err := builder.ClusterScraper.GetPodControllerInfo(pod, true)
		if err != nil || util.IsOwnerInfoEmpty(ownerInfo) {
			// The pod does not have a controller
			continue
		}
		if _, found := controllers[ownerInfo.Uid]; found {
			// We've already visited this controller
			if util.PodIsReady(pod) {
				readyPods++
			}
			continue
		}
		// Get the cached controller from the controller cache in ClusterSummary
		controller, found := builder.ClusterSummary.ControllerMap[ownerInfo.Uid]
		if !found {
			// No cached controller
			continue
		}
		if controller.Replicas == nil || *controller.Replicas < 1 {
			// No valid replicas
			continue
		}
		controllers[controller.UID] = controller
		if util.PodIsReady(pod) {
			readyPods++
		}
	}
	if len(controllers) == 0 {
		glog.Errorf("No controllers are found for any pod that provides to service %v.", serviceName)
		return nil
	}
	// The list of pods that provide to the service may belong to multiple controllers
	// Determine capacity by getting the max of all replicas
	var replicas int64
	for _, controller := range controllers {
		if *controller.Replicas > replicas {
			replicas = *controller.Replicas
		}
	}
	used := float64(readyPods)
	capacity := float64(replicas)
	if used > capacity {
		glog.Warningf("Number of replicas for %v has used value %f larger than capacity %f",
			serviceName, used, capacity)
	}
	commoditySold, err := sdkbuilder.
		NewCommodityDTOBuilder(proto.CommodityDTO_NUMBER_REPLICAS).
		Used(used).
		Capacity(capacity).
		// Deactivate the commodity so it is not sent to the market
		Active(false).
		Create()
	if err != nil {
		return err
	}
	ebuilder.SellsCommodity(commoditySold)
	return nil
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
		return fmt.Errorf("failed to find any provider for service")
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
		return nil, fmt.Errorf("no commodity found")
	}

	return commoditiesBoughtFromApp, nil
}

// Get the UUID property of the service for stitching purpose
func getUUIDProperty(uuid string) *proto.EntityDTO_EntityProperty {
	ns := stitching.DefaultPropertyNamespace
	attr := string(stitching.UUID)
	return &proto.EntityDTO_EntityProperty{
		Namespace: &ns,
		Name:      &attr,
		Value:     &uuid,
	}
}

// Get the IP property appended with K8s service ID for stitching purpose
// Format for stitching attribute for service looks like : "Service-[IP1], Service-[IP1]-[svcUID], Service-[IP2], Service-[IP2]-[svcUID]"
func getIPProperty(pods []*api.Pod, svcUID string) *proto.EntityDTO_EntityProperty {
	ns := stitching.DefaultPropertyNamespace
	attr := stitching.AppStitchingAttr
	ips := []string{}
	for _, pod := range pods {
		ips = append(ips, servicePrefix+"-"+pod.Status.PodIP,
			servicePrefix+"-"+pod.Status.PodIP+"-"+util.ParseSvcUID(svcUID))
	}
	ip := strings.Join(ips, ",")
	ipProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &ns,
		Name:      &attr,
		Value:     &ip,
	}

	return ipProperty
}
