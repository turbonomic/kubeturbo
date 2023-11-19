package dtofactory

import (
	"fmt"

	api "k8s.io/api/core/v1"

	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"

	sdkbuilder "github.ibm.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/stitching"
)

var (
	applicationResourceCommodityBought = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
	}
)

type applicationEntityDTOBuilder struct {
	generalBuilder
	podClusterIDToServiceMap map[string]*api.Service
	ClusterScraper           *cluster.ClusterScraper
}

// Builder to build DTOs for application running on each container
// Metric Sink provides the metrics saved by the discovery worker.
// podClusterIDToServiceMap provides the service ID for the service associated with the applications running on the pods.
// ClusterScraper Interface provides the implentation to get kubernetes service Id
func NewApplicationEntityDTOBuilder(sink *metrics.EntityMetricSink,
	podClusterIDToServiceMap map[string]*api.Service, clusterScraper *cluster.ClusterScraper) *applicationEntityDTOBuilder {
	return &applicationEntityDTOBuilder{
		generalBuilder:           newGeneralBuilder(sink),
		podClusterIDToServiceMap: podClusterIDToServiceMap,
		ClusterScraper:           clusterScraper,
	}
}

func (builder *applicationEntityDTOBuilder) BuildEntityDTO(pod *api.Pod) ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	podFullName := util.GetPodClusterID(pod)
	podId := string(pod.UID)
	podMId := util.PodMetricIdAPI(pod)
	nodeCPUFrequency, err := builder.getNodeCPUFrequencyViaPod(pod)
	if err != nil {
		glog.Warningf("Could not get node cpu frequency for pod[%s]."+
			"\nHosted application usage data may not reflect right Mhz values: %v", podFullName, err)
	}
	svcUID, err := builder.ClusterScraper.GetKubernetesServiceID()
	if err != nil {
		glog.Warningf("Could not get Kubernetes service ID: %v", err)
	}
	for i := range pod.Spec.Containers {
		//1. Id and Name
		container := &(pod.Spec.Containers[i])
		containerId := util.ContainerIdFunc(podId, i)
		appId := util.ApplicationIdFunc(containerId)
		containerMId := util.ContainerMetricId(podMId, container.Name)
		appMId := util.ApplicationMetricId(containerMId)

		displayName := util.ApplicationDisplayName(podFullName, container.Name)

		ebuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_APPLICATION_COMPONENT, appId).
			DisplayName(displayName)

		//2. sold commodities: transaction and responseTime
		commoditiesSold, err := builder.getCommoditiesSold(pod, i)
		if err != nil {
			glog.Warningf("Skip creating Application(%s) entityDTO: %v", displayName, err)
			continue
		}
		ebuilder.SellsCommodities(commoditiesSold)

		//3. bought commodities: vcpu/vmem/application
		commoditiesBought, err := builder.getApplicationCommoditiesBought(appMId, podFullName, containerId, nodeCPUFrequency)
		if err != nil {
			glog.Warningf("Skip creating Application(%s) entityDTO: %v", displayName, err)
			continue
		}
		provider := sdkbuilder.CreateProvider(proto.EntityDTO_CONTAINER, containerId)
		ebuilder.Provider(provider).BuysCommodities(commoditiesBought)

		//4. set properties
		properties := builder.getApplicationProperties(pod, i, svcUID)
		ebuilder.WithProperties(properties)

		// controllability of applications should not be dictated by mirror pods modeled as daemon pods
		// because they cannot be controlled through the API server
		controllable := util.Controllable(pod, false)
		monitored := true
		powerState := proto.EntityDTO_POWERED_ON
		if !util.PodIsReady(pod) {
			controllable = false
			monitored = false
			powerState = proto.EntityDTO_POWERSTATE_UNKNOWN
		}

		//5. build the entityDTO
		truep := true
		appType := util.GetAppType(pod)
		entityDTO, err := ebuilder.
			ApplicationData(&proto.EntityDTO_ApplicationData{
				Type:                    &appType,
				IpAddress:               &(pod.Status.PodIP),
				HostingNodeCpuFrequency: &nodeCPUFrequency,
			}).
			ConsumerPolicy(&proto.EntityDTO_ConsumerPolicy{
				ProviderMustClone: &truep,
				Controllable:      &controllable,
			}).
			Monitored(monitored).
			WithPowerState(powerState).
			Create()
		if err != nil {
			glog.Errorf("Failed to build Application entityDTO based on application %s: %s", displayName, err)
			continue
		}
		glog.V(4).Infof("App DTO: %++v", entityDTO)
		result = append(result, entityDTO)
	}

	return result, nil
}

// applicationEntity sells transaction and responseTime
func (builder *applicationEntityDTOBuilder) getCommoditiesSold(pod *api.Pod, index int) ([]*proto.CommodityDTO, error) {
	var result []*proto.CommodityDTO

	podClusterId := util.GetPodClusterID(pod)
	svc := builder.podClusterIDToServiceMap[podClusterId]
	// commodity key using service Id
	var key string

	// Service is associated with the pod, then the commodities key is the service Id,
	// Else, it is computed using the container IP
	if svc != nil {
		// Use svc UID as key to be universally unique across different clusters
		key = string(svc.UID)
	} else {
		key = pod.Status.PodIP
	}

	// Sold commodity Key is appended with the container index,
	// to distinguish between the main application serving the virtual application service
	// and the sidecar application in the pod
	if index > 0 {
		key = fmt.Sprintf("%s-%d", key, index)
	}

	// Application Access Commodity in lieu of Transaction and ResponseTime commodities
	ebuilder := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_APPLICATION).Key(key)

	tranCommodity, err := ebuilder.Create()
	if err != nil {
		glog.Errorf("Failed to create application(%s) commodity sold:%v", key, err)
		return nil, err
	}
	result = append(result, tranCommodity)

	return result, nil
}

// Build the bought commodities by each application.
// An application buys vCPU, vMem and Application commodity from a container.
func (builder *applicationEntityDTOBuilder) getApplicationCommoditiesBought(appMId, podName, containerId string, cpuFrequency float64) ([]*proto.CommodityDTO, error) {
	var commoditiesBought []*proto.CommodityDTO

	converter := NewConverter().Set(func(input float64) float64 { return util.MetricMilliToUnit(input) * cpuFrequency }, metrics.CPU)
	// Resource commodities.
	resourceCommoditiesBought := builder.getResourceCommoditiesBought(metrics.ApplicationType, appMId, applicationResourceCommodityBought, converter, nil)
	if len(resourceCommoditiesBought) != len(applicationResourceCommodityBought) {
		return nil, fmt.Errorf("mismatch num of commidities (%d Vs. %d) for application:%s, %s",
			len(resourceCommoditiesBought), len(applicationResourceCommodityBought), podName, appMId)
	}
	commoditiesBought = append(commoditiesBought, resourceCommoditiesBought...)

	// Application commodity
	applicationCommBought, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_APPLICATION).
		Key(containerId).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesBought = append(commoditiesBought, applicationCommBought)
	return commoditiesBought, nil
}

// Get the properties of the pod. This includes property related to application cluster property.
func (builder *applicationEntityDTOBuilder) getApplicationProperties(pod *api.Pod, index int, svcUID string) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	// additional node cluster info property.
	appProperties := property.AddHostingPodProperties(pod.Namespace, pod.Name, index)
	ns := stitching.DefaultPropertyNamespace
	attr := stitching.AppStitchingAttr
	value := getAppStitchingProperty(pod, index, svcUID)
	stitchingProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &ns,
		Name:      &attr,
		Value:     &value,
	}

	properties = append(properties, appProperties...)
	properties = append(properties, stitchingProperty)

	return properties
}

// Get the stitching property for Application.
func getAppStitchingProperty(pod *api.Pod, index int, svcUID string) string {
	// For the container with index 0, the property is the pod ip with kubernetes service Id "[IP],[IP]-[svcUID]"
	// For other containers, the container index is appended with hypen along with kubernetes service Id, i.e., "[IP]-[Index], [IP]-[Index]-[svcUID]"
	property := pod.Status.PodIP
	if index > 0 {
		property = fmt.Sprintf("%s-%d", property, index)
	}
	if svcUID != "" {
		property = fmt.Sprintf("%s,%s-%s", property, property, svcUID)
	}
	return property
}
