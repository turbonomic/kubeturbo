package dtofactory

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type quotaEntityDTOBuilder struct {
	QuotaMap         map[string]*repository.KubeQuota
	nodeMapByUID     map[string]*repository.KubeNode
	stitchingManager *stitching.StitchingManager
	nodeNames        []string
}

func NewQuotaEntityDTOBuilder(quotaMap map[string]*repository.KubeQuota,
	nodeMap map[string]*repository.KubeNode,
	ptype stitching.StitchingPropertyType) *quotaEntityDTOBuilder {

	m := stitching.NewStitchingManager(ptype)
	builder := &quotaEntityDTOBuilder{
		QuotaMap:         quotaMap,
		nodeMapByUID:     make(map[string]*repository.KubeNode),
		stitchingManager: m,
		nodeNames:        []string{},
	}

	for _, node := range nodeMap {
		builder.nodeMapByUID[node.UID] = node
	}
	builder.setUpStitchManager()

	return builder
}

func (builder *quotaEntityDTOBuilder) setUpStitchManager() {
	m := builder.stitchingManager

	glog.V(3).Infof("stitchType: %v", m.GetStitchType())
	//1. set UUID getter by one k8s.Node.providerID
	if m.GetStitchType() == stitching.UUID {
		glog.V(3).Info("Begin to setup stitchManager UUID getter.")
	}

	//2. store the stitching values
	builder.nodeNames = []string{}
	for _, node := range builder.nodeMapByUID {
		if node != nil {
			providerId := node.Node.Spec.ProviderID
			m.SetNodeUuidGetterByProvider(providerId)
			m.StoreStitchingValue(node.Node)
			builder.nodeNames = append(builder.nodeNames, node.Node.Name)
		}
	}
	return
}

// Build entityDTOs based on the given node list.
func (builder *quotaEntityDTOBuilder) BuildEntityDTOs() ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO

	for _, quota := range builder.QuotaMap {
		// id.
		quotaID := quota.UID
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_VIRTUAL_DATACENTER, quotaID)

		// display name.
		displayName := quota.Name
		entityDTOBuilder.DisplayName(displayName)

		// commodities sold.
		commoditiesSold, err := builder.getQuotaCommoditiesSold(quota)
		if err != nil {
			glog.Errorf("Error creating commoditiesSold for %s: %s", quota.Name, err)
			continue
		}
		entityDTOBuilder.SellsCommodities(commoditiesSold)

		// commodities bought.
		for _, kubeProvider := range quota.ProviderMap {
			commoditiesBought, err := builder.getQuotaCommoditiesBought(displayName, kubeProvider)
			if err != nil {
				glog.Errorf("Error creating commoditiesBought for quota %s: %s", displayName, err)
				continue
			}

			provider := sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, kubeProvider.UID)
			entityDTOBuilder = entityDTOBuilder.Provider(provider)
			entityDTOBuilder.BuysCommodities(commoditiesBought)
		}

		// stitching properties
		properties, err := builder.getQuotaProperty()
		if err != nil {
			glog.Errorf("Failed to build stitching property for Quota: %v, %v", displayName, err)
			continue
		}

		entityDTOBuilder = entityDTOBuilder.WithProperties(properties)

		entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

		// build entityDTO.
		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build Quota entityDTO: %s", err)
			continue
		}

		result = append(result, entityDto)
		glog.V(4).Infof("quota dto : %++v\n", entityDto)
	}
	return result, nil
}

func (builder *quotaEntityDTOBuilder) getQuotaCommoditiesSold(quota *repository.KubeQuota) ([]*proto.CommodityDTO, error) {
	var resourceCommoditiesSold []*proto.CommodityDTO
	for resourceType, resource := range quota.AllocationResources {
		cType, exist := rTypeMapping[resourceType]
		if !exist {
			// this error message is commented out because the commodity
			// for cpu and mem request is not currently supported
			//glog.Errorf("Commodity type %s sold by %s is not supported", resourceType, entityType)
			continue
		}
		capacityValue := resource.Capacity
		usedValue := resource.Used
		// For CPU resources, convert the capacity and usage values expressed in
		// number of cores to MHz
		if metrics.IsCPUType(resourceType) && quota.AverageNodeCpuFrequency > 0.0 {
			// always modify the capacity value
			newVal := capacityValue * quota.AverageNodeCpuFrequency
			glog.V(4).Infof("%s::%s: changed capacity from  [%f cores] to %f MHz",
				quota.Name, resourceType, capacityValue, newVal)
			capacityValue = newVal

			// Modify the used value only if the quota is set for the resource type.
			// This is because the used value is obtained from the resource quota objects
			// and represented in number of cores.
			// If quota is not set for the resource type, the usage is sum of resource
			// usages for all the pods in the namespace and
			// has been converted to MHz using the hosting node's CPU frequency
			if quota.AllocationDefined[resourceType] {
				newVal := usedValue * quota.AverageNodeCpuFrequency
				glog.V(4).Infof("%s::%s quota defined, changed usage from [%f cores] to %f MHz",
					quota.Name, resourceType, usedValue, newVal)
				usedValue = newVal
			}
		}

		commSoldBuilder := sdkbuilder.NewCommodityDTOBuilder(cType)
		commSoldBuilder.Used(usedValue)
		commSoldBuilder.Capacity(capacityValue)
		commSoldBuilder.Resizable(true)
		commSoldBuilder.Key(quota.UID)

		commSold, err := commSoldBuilder.Create()
		if err != nil {
			glog.Errorf("%s : Failed to build commodity sold: %s", quota.Name, err)
			continue
		}
		resourceCommoditiesSold = append(resourceCommoditiesSold, commSold)
	}
	return resourceCommoditiesSold, nil
}

func (builder *quotaEntityDTOBuilder) getQuotaCommoditiesBought(quotaName string, provider *repository.KubeResourceProvider) ([]*proto.CommodityDTO, error) {

	if provider == nil {
		return nil, fmt.Errorf("%s: null provider\n", quotaName)
	}

	var commoditiesBought []*proto.CommodityDTO
	for resourceType, resource := range provider.BoughtAllocation {
		cType, exist := rTypeMapping[resourceType]
		if !exist {
			// this error message is commented out because the commodity
			// for cpu and mem request is not currently supported
			//glog.Errorf("Commodity type %s bought by %s is not supported", resourceType, entityType)
			continue
		}

		commBoughtBuilder := sdkbuilder.NewCommodityDTOBuilder(cType)
		usedValue := resource.Used
		commBoughtBuilder.Used(usedValue)
		commBoughtBuilder.Resizable(true)
		commBoughtBuilder.Key(provider.UID)

		commBought, err := commBoughtBuilder.Create()
		if err != nil {
			glog.Errorf("%s : Failed to build commodity bought: %s", quotaName, err)
			continue
		}
		commoditiesBought = append(commoditiesBought, commBought)
	}

	return commoditiesBought, nil
}

func (builder *quotaEntityDTOBuilder) getQuotaProperty() ([]*proto.EntityDTO_EntityProperty, error) {
	var result []*proto.EntityDTO_EntityProperty

	//1. build stitching property (stitch VDC to VMs)
	//   TODO: this property is same to all the VDCs. Better build it in advance, and shared by the VDCs.
	property, err := builder.stitchingManager.BuildDTOLayerOverProperty(builder.nodeNames)
	if err != nil {
		glog.Errorf("Failed to build QuotaStitchingProperty: %v", err)
		return result, err
	}
	result = append(result, property)

	return result, nil
}
