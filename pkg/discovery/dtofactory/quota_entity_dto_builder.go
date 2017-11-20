package dtofactory

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/golang/glog"
	"fmt"
)

type quotaEntityDTOBuilder struct {
	QuotaMap	map[string]*repository.KubeQuota
}

func NewQuotaEntityDTOBuilder(quotaMap map[string]*repository.KubeQuota) *quotaEntityDTOBuilder {
	return &quotaEntityDTOBuilder{QuotaMap: quotaMap,}
}

// Build entityDTOs based on the given node list.
func (builder *quotaEntityDTOBuilder) BuildEntityDTOs() ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO

	for _, quota := range builder.QuotaMap {
		// id.
		quotaID := string(quota.Name)
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
		for _, kubeProvider := range quota.ProviderMap{
			commoditiesBought, err := builder.getQuotaCommoditiesBought(kubeProvider)
			if err != nil {
				glog.Errorf("Error creating commoditiesBought for quota %s: %s", displayName, err)
				continue
			}

			provider := sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, kubeProvider.UID)
			entityDTOBuilder = entityDTOBuilder.Provider(provider)
			entityDTOBuilder.BuysCommodities(commoditiesBought)
		}

		// build entityDTO.
		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build Quota entityDTO: %s", err)
			continue
		}

		result = append(result, entityDto)
		//fmt.Printf("QUOTA DTO : %++v\n", entityDto)
	}
	return result, nil
}

func (builder *quotaEntityDTOBuilder) getQuotaCommoditiesSold(quota *repository.KubeQuota) ([]*proto.CommodityDTO, error){
	var resourceCommoditiesSold []*proto.CommodityDTO
	for resourceType, resource := range quota.AllocationResources {
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

func (builder *quotaEntityDTOBuilder) getQuotaCommoditiesBought(provider *repository.KubeResourceProvider) ([]*proto.CommodityDTO, error) {

	if provider == nil {
		return nil, fmt.Errorf("null provider\n")
	}

	var commoditiesBought []*proto.CommodityDTO
	for resourceType, resource:= range provider.BoughtAllocation {
		cType, exist := rTypeMapping[resourceType]
		if !exist {
			//glog.Errorf("Commodity type %s bought by %s is not supported", resourceType, entityType)
			continue
		}

		commBoughtBuilder := sdkbuilder.NewCommodityDTOBuilder(cType)
		usedValue := resource.Used
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
