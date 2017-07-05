package dtofactory

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/probe"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const (
	AppPrefix string = "App-"
)

var (
	applicationResourceCommoditySold = []metrics.ResourceType{
		metrics.Transaction,
	}

	applicationResourceCommodityBought = []metrics.ResourceType{
		metrics.CPU,
		metrics.Memory,
	}
)

type applicationEntityDTOBuilder struct {
	generalBuilder
	nodeNameUIDMap map[string]string
}

func NewApplicationEntityDTOBuilder(sink *metrics.EntityMetricSink) *applicationEntityDTOBuilder {
	return &applicationEntityDTOBuilder{
		generalBuilder: newGeneralBuilder(sink),
	}
}

// Build entityDTOs based on the given pod list.
func (builder *applicationEntityDTOBuilder) BuildEntityDTOs(pods []runtime.Object) ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	for _, p := range pods {
		pod, ok := p.(*api.Pod)
		if !ok {
			glog.Warningf("%v is not a pod, which is required to build application entityDTO.", pod.GetObjectKind())
		}

		// id. Application entity dto ID is consisted of appPrefix and pod UID.
		appID := getApplicationID(string(pod.UID))
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_APPLICATION, appID)

		// display name
		displayName := AppPrefix + util.GetPodClusterID(pod)
		entityDTOBuilder.DisplayName(displayName)

		// commodity sold
		commoditiesSold, err := builder.getApplicationCommoditiesSold(pod)
		if err != nil {
			glog.Errorf("Failed to create commodities sold by application %s: %s", displayName, err)
		}
		entityDTOBuilder.SellsCommodities(commoditiesSold)

		// commodity bought
		provider := sdkbuilder.CreateProvider(proto.EntityDTO_CONTAINER_POD, string(pod.UID))
		entityDTOBuilder = entityDTOBuilder.Provider(provider)
		commoditiesBought, err := builder.getApplicationCommoditiesBought(pod)
		if err != nil {
			glog.Errorf("Failed to create commodities bought by application %s: %s", displayName, err)
		}
		entityDTOBuilder.BuysCommodities(commoditiesBought)

		// entities' properties.
		properties := builder.getApplicationProperties(pod)
		entityDTOBuilder.WithProperties(properties)

		if !util.Monitored(pod) {
			entityDTOBuilder.Monitored(false)
		}

		appType := probe.GetAppType(pod)
		entityDTOBuilder.ApplicationData(&proto.EntityDTO_ApplicationData{
			Type: &appType,
		})

		// build entityDTO
		entityDTO, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build Application entityDTO based on application %s: %s", displayName, err)
			continue
		}

		result = append(result, entityDTO)
	}

	return result, nil
}

// Build the sold commodityDTOs by each application.
// An application sells: Transaction commodity.
func (builder *applicationEntityDTOBuilder) getApplicationCommoditiesSold(pod *api.Pod) ([]*proto.CommodityDTO, error) {

	// Cluster commodity.
	clusterMetricUID := metrics.GenerateEntityStateMetricUID(task.ClusterType, "", metrics.Cluster)
	clusterInfo, err := builder.metricsSink.GetMetric(clusterMetricUID)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s used for current Kubernetes Cluster", metrics.Cluster)
	}
	clusterCommodityKey, ok := clusterInfo.GetValue().(string)
	if !ok {
		return nil, errors.New("Failed to get cluster ID")
	}
	glog.V(4).Infof("Cluster key is %s", clusterCommodityKey)

	var commoditiesSold []*proto.CommodityDTO
	// As all the application commodities are actually retrieved from pods, so the key is also from pod.
	key := util.PodKeyFunc(pod)

	// attr
	attributeSetter := NewCommodityAttrSetter()
	//attributeSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Resizable(true) }, metrics.CPU, metrics.Memory)
	transactionCommKey := FindTransactionCommodityKey(pod, clusterCommodityKey)
	attributeSetter.Add(func(commBuilder *sdkbuilder.CommodityDTOBuilder) { commBuilder.Key(transactionCommKey) }, metrics.Transaction)

	// transaction
	resourceCommoditiesSold, err := builder.getResourceCommoditiesSold(task.ApplicationType, key, applicationResourceCommoditySold, nil, attributeSetter)
	if err != nil {
		return nil, err
	}
	commoditiesSold = append(commoditiesSold, resourceCommoditiesSold...)

	return commoditiesSold, nil
}

func FindTransactionCommodityKey(pod *api.Pod, clusterID string) string {
	appType := probe.GetAppType(pod)
	return appType + "-" + clusterID
}

// Build the bought commodities by each application.
// An application buys vCPU, vMem and Application commodity from a pod.
func (builder *applicationEntityDTOBuilder) getApplicationCommoditiesBought(pod *api.Pod) ([]*proto.CommodityDTO, error) {
	var commoditiesBought []*proto.CommodityDTO
	// As all the application commodities are actually retrieved from pods, so the key is also from pod.
	key := util.PodKeyFunc(pod)

	// get cpu frequency
	cpuFrequencyUID := metrics.GenerateEntityStateMetricUID(task.NodeType, util.NodeKeyFromPodFunc(pod), metrics.CpuFrequency)
	cpuFrequencyMetric, err := builder.metricsSink.GetMetric(cpuFrequencyUID)
	if err != nil {
		glog.Errorf("Failed to get cpu frequency from sink for node %s: %s", util.NodeKeyFromPodFunc(pod), err)
	}
	cpuFrequency := cpuFrequencyMetric.GetValue().(float64)
	// cpu and cpu provisioned needs to be converted from number of cores to frequency.
	converter := NewConverter().Set(func(input float64) float64 { return input * cpuFrequency }, metrics.CPU)

	// Resource commodities.
	resourceCommoditiesBought, err := builder.getResourceCommoditiesBought(task.ApplicationType, key, applicationResourceCommodityBought, converter, nil)
	if err != nil {
		return nil, err
	}
	commoditiesBought = append(commoditiesBought, resourceCommoditiesBought...)

	// Application commodity
	applicationCommBought, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_APPLICATION).
		Key(string(pod.UID)).
		Create()
	if err != nil {
		return nil, err
	}
	commoditiesBought = append(commoditiesBought, applicationCommBought)
	return commoditiesBought, nil
}

// Get the properties of the pod. This includes property related to application cluster property.
func (builder *applicationEntityDTOBuilder) getApplicationProperties(pod *api.Pod) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	// additional node cluster info property.
	appProperties := probe.BuildAppProperties(pod.Namespace, pod.Name)
	properties = append(properties, appProperties...)

	return properties
}

// the uuid that is used to create entityDTO and is unique in Turbonomic system.
func getApplicationID(id string) string {
	return AppPrefix + id
}
