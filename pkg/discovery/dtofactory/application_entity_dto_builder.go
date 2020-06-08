package dtofactory

import (
	"fmt"
	api "k8s.io/api/core/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
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
}

// Builder to build DTOs for application running on each container
// Metric Sink provides the metrics saved by the discovery worker.
// podClusterIDToServiceMap provides the service ID for the service associated with the applications running on the pods.
func NewApplicationEntityDTOBuilder(sink *metrics.EntityMetricSink,
	podClusterIDToServiceMap map[string]*api.Service) *applicationEntityDTOBuilder {
	return &applicationEntityDTOBuilder{
		generalBuilder:           newGeneralBuilder(sink),
		podClusterIDToServiceMap: podClusterIDToServiceMap,
	}
}

// get hosting node cpu frequency
func (builder *applicationEntityDTOBuilder) getNodeCPUFrequency(pod *api.Pod) (float64, error) {
	key := util.NodeKeyFromPodFunc(pod)
	cpuFrequencyUID := metrics.GenerateEntityStateMetricUID(metrics.NodeType, key, metrics.CpuFrequency)
	cpuFrequencyMetric, err := builder.metricsSink.GetMetric(cpuFrequencyUID)
	if err != nil {
		err := fmt.Errorf("Failed to get cpu frequency from sink for node %s: %v", key, err)
		glog.Error(err)
		return 0.0, err
	}

	cpuFrequency := cpuFrequencyMetric.GetValue().(float64)
	return cpuFrequency, nil
}

func (builder *applicationEntityDTOBuilder) BuildEntityDTO(pod *api.Pod) ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	podFullName := util.GetPodClusterID(pod)
	nodeCPUFrequency, err := builder.getNodeCPUFrequency(pod)
	if err != nil {
		return nil, fmt.Errorf("failed to build application DTOs for pod[%s]: %v", podFullName, err)
	}
	podId := string(pod.UID)
	podMId := util.PodMetricIdAPI(pod)

	for i := range pod.Spec.Containers {
		//1. Id and Name
		container := &(pod.Spec.Containers[i])
		containerId := util.ContainerIdFunc(podId, i)
		appId := util.ApplicationIdFunc(containerId)
		containerMId := util.ContainerMetricId(podMId, container.Name)
		appMId := util.ApplicationMetricId(containerMId)

		displayName := util.ApplicationDisplayName(podFullName, container.Name)

		ebuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_APPLICATION, appId).
			DisplayName(displayName)

		//2. sold commodities: transaction and responseTime
		commoditiesSold, err := builder.getCommoditiesSold(pod, i)
		if err != nil {
			glog.Errorf("Failed to create Application(%s) entityDTO: %v", displayName, err)
			continue
		}
		ebuilder.SellsCommodities(commoditiesSold)

		//3. bought commodities: vcpu/vmem/application
		commoditiesBought, err := builder.getApplicationCommoditiesBought(appMId, podFullName, containerId, nodeCPUFrequency)
		if err != nil {
			glog.Errorf("Failed to create Application(%s) entityDTO: %v", displayName, err)
			continue
		}
		provider := sdkbuilder.CreateProvider(proto.EntityDTO_CONTAINER, containerId)
		ebuilder.Provider(provider).BuysCommodities(commoditiesBought)

		//4. set properties
		properties := builder.getApplicationProperties(pod, i)
		ebuilder.WithProperties(properties)

		truep := true
		controllable := util.Controllable(pod)
		ebuilder.ConsumerPolicy(&proto.EntityDTO_ConsumerPolicy{
			ProviderMustClone: &truep,
			Controllable:      &controllable,
		})

		appType := util.GetAppType(pod)
		ebuilder.ApplicationData(&proto.EntityDTO_ApplicationData{
			Type:      &appType,
			IpAddress: &(pod.Status.PodIP),
		})

		ebuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

		//5. build the entityDTO
		entityDTO, err := ebuilder.Create()
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

	converter := NewConverter().Set(func(input float64) float64 { return input * cpuFrequency }, metrics.CPU)

	// Resource commodities.
	resourceCommoditiesBought, err := builder.getResourceCommoditiesBought(metrics.ApplicationType, appMId, applicationResourceCommodityBought, converter, nil)
	if err != nil {
		return nil, err
	}
	if len(resourceCommoditiesBought) != len(applicationResourceCommodityBought) {
		err = fmt.Errorf("mismatch num of commidities (%d Vs. %d) for application:%s, %s", len(resourceCommoditiesBought), len(applicationResourceCommodityBought), podName, appMId)
		glog.Error(err)
		return nil, err
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
func (builder *applicationEntityDTOBuilder) getApplicationProperties(pod *api.Pod, index int) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	// additional node cluster info property.
	appProperties := property.AddHostingPodProperties(pod.Namespace, pod.Name, index)

	ns := stitching.DefaultPropertyNamespace
	attr := stitching.AppStitchingAttr
	value := getAppStitchingProperty(pod, index)
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
func getAppStitchingProperty(pod *api.Pod, index int) string {
	// For the container with index 0, the property is the pod ip.
	// For other containers, the container index is appended with hypen, i.e., [IP]-[Index]
	property := pod.Status.PodIP
	if index > 0 {
		property = fmt.Sprintf("%s-%d", pod.Status.PodIP, index)
	}

	return property
}
