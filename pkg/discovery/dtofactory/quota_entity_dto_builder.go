package dtofactory

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/golang/glog"
)

type quotaEntityDTOBuilder struct {
	kubeCluster *repository.KubeCluster
}

func NewQuotaEntityDTOBuilder(kubeCluster *repository.KubeCluster) *quotaEntityDTOBuilder {
	return &quotaEntityDTOBuilder{kubeCluster: kubeCluster,}
}

// Build entityDTOs based on the given node list.
func (builder *quotaEntityDTOBuilder) BuildEntityDTOs(quotaMetrics map[string]*repository.QuotaMetrics) ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	quotaMap := builder.kubeCluster.GetQuotas()

	for _, quota := range quotaMap {
		// id.
		quotaID := string(quota.Name)
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_VIRTUAL_DATACENTER, quotaID)

		// display name.
		displayName := quota.Name
		entityDTOBuilder.DisplayName(displayName)

		// commodities sold.
		commoditiesSold, err := builder.getQuotaCommoditiesSold(quota)
		if err != nil {
			glog.Errorf("Error when create commoditiesSold for %s: %s", quota.Name, err)
			continue
		}
		entityDTOBuilder.SellsCommodities(commoditiesSold)

		// commodities bought.
		quotaMetrics, exists := quotaMetrics[quota.Name]
		if exists {
			for nodeName, providerMap := range quotaMetrics.AllocationBoughtMap {
				commoditiesBought, err := builder.getQuotaCommoditiesBought(providerMap)
				if err != nil {
					glog.Errorf("Error when create commoditiesBought for pod %s: %s", displayName, err)
					continue
				}
				kubeNode, exists := builder.kubeCluster.Nodes[nodeName]
				if !exists {
					glog.Errorf("Error when create commoditiesBought for pod %s:" +
						" Cannot find uuid for provider %s node.",
						displayName, nodeName, err)
					continue
				}
				providerNodeUID := kubeNode.UID
				provider := sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, providerNodeUID)
				entityDTOBuilder = entityDTOBuilder.Provider(provider)
				entityDTOBuilder.BuysCommodities(commoditiesBought)
			}
		}

		// build entityDTO.
		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build Pod entityDTO: %s", err)
			continue
		}

		result = append(result, entityDto)
		//fmt.Printf("QUOTA DTO : %++v\n", entityDto)
	}
	return result, nil
}

func (builder *quotaEntityDTOBuilder) getQuotaCommoditiesSold(quota *repository.KubeQuota) ([]*proto.CommodityDTO, error){
	//entityType := metrics.QuotaType
	var resourceCommoditiesSold []*proto.CommodityDTO
	for resourceType, resource := range quota.ComputeResources {
		cType, exist := rTypeMapping[resourceType]
		if !exist {
			//glog.Errorf("Commodity type %s sold by %s is not supported", resourceType, entityType)
			continue
		}
		commSoldBuilder := sdkbuilder.NewCommodityDTOBuilder(cType)
		usedValue := resource.Used
		commSoldBuilder.Used(usedValue)
		capacityValue := resource.Capacity
		commSoldBuilder.Capacity(capacityValue)
		commSoldBuilder.Resizable(true)

		commSold, err := commSoldBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build commodity sold: %s", err)
			continue
		}
		resourceCommoditiesSold = append(resourceCommoditiesSold, commSold)
	}
	return resourceCommoditiesSold, nil
}

func (builder *quotaEntityDTOBuilder) getQuotaCommoditiesBought(providerResourceMap map[metrics.ResourceType]float64) ([]*proto.CommodityDTO, error) {
	var commoditiesBought []*proto.CommodityDTO
	//entityType := metrics.QuotaType
	for resourceType, resourceUsed := range providerResourceMap {
		cType, exist := rTypeMapping[resourceType]
		if !exist {
			//glog.Errorf("Commodity type %s bought by %s is not supported", resourceType, entityType)
			continue
		}

		commBoughtBuilder := sdkbuilder.NewCommodityDTOBuilder(cType)
		usedValue := resourceUsed
		commBoughtBuilder.Used(usedValue)
		commBoughtBuilder.Resizable(true)

		commBought, err := commBoughtBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build commodity sold: %s", err)
			continue
		}
		commoditiesBought = append(commoditiesBought, commBought)
	}
	return commoditiesBought, nil
}
