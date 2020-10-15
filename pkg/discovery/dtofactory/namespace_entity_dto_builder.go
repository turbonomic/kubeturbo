package dtofactory

import (
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type namespaceEntityDTOBuilder struct {
	NamespaceMap map[string]*repository.KubeNamespace
}

func NewNamespaceEntityDTOBuilder(namespaceMap map[string]*repository.KubeNamespace) *namespaceEntityDTOBuilder {
	builder := &namespaceEntityDTOBuilder{
		NamespaceMap: namespaceMap,
	}
	return builder
}

// Build entityDTOs based on the given node list.
func (builder *namespaceEntityDTOBuilder) BuildEntityDTOs() ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO

	for _, namespace := range builder.NamespaceMap {
		// id.
		namespaceID := namespace.UID
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_NAMESPACE, namespaceID)

		// display name.
		displayName := namespace.Name
		entityDTOBuilder.DisplayName(displayName)

		// Resource commodities sold.
		commoditiesSold, err := builder.getQuotaCommoditiesSold(namespace)
		if err != nil {
			glog.Errorf("Error creating commoditiesSold for %s: %s", namespace.Name, err)
			continue
		}
		entityDTOBuilder.SellsCommodities(commoditiesSold)

		commoditiesBought, err := builder.getCommoditiesBought(namespace)
		if err != nil {
			glog.Errorf("Error creating commoditiesBought for %s: %v", namespace.Name, err)
			continue
		}
		entityDTOBuilder.Provider(sdkbuilder.CreateProvider(proto.EntityDTO_K8S_CLUSTER, namespace.ClusterName)).BuysCommodities(commoditiesBought)
		// Set movable false to avoid moving Namespace across Clusters
		entityDTOBuilder.IsMovable(proto.EntityDTO_K8S_CLUSTER, false)

		// Namespace entity cannot be provisioned or suspended by Turbonomic analysis
		entityDTOBuilder.IsProvisionable(false)
		entityDTOBuilder.IsSuspendable(false)

		entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

		// build entityDTO.
		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build Namespace entityDTO: %s", err)
			continue
		}

		result = append(result, entityDto)
		glog.V(4).Infof("Namespace DTO: %+v", entityDto)
	}
	return result, nil
}

func (builder *namespaceEntityDTOBuilder) getQuotaCommoditiesSold(kubeNamespace *repository.KubeNamespace) ([]*proto.CommodityDTO, error) {
	var resourceCommoditiesSold []*proto.CommodityDTO
	for resourceType, resource := range kubeNamespace.AllocationResources {
		cType, exist := rTypeMapping[resourceType]
		if !exist {
			continue
		}
		capacityValue := resource.Capacity
		usedValue := resource.Used
		// For CPU resources, convert the capacity and usage values expressed in
		// number of cores to MHz
		if metrics.IsCPUType(resourceType) && kubeNamespace.AverageNodeCpuFrequency > 0.0 {
			if capacityValue != repository.DEFAULT_METRIC_CAPACITY_VALUE {
				// Modify the capacity value from cores to MHz if capacity is not default infinity
				newVal := capacityValue * kubeNamespace.AverageNodeCpuFrequency
				glog.V(4).Infof("Changing capacity of %s::%s from %f cores to %f MHz",
					kubeNamespace.Name, resourceType, capacityValue, newVal)
				capacityValue = newVal
			}
		}

		commSoldBuilder := sdkbuilder.NewCommodityDTOBuilder(cType)
		commSoldBuilder.Used(usedValue)
		commSoldBuilder.Peak(usedValue)
		commSoldBuilder.Capacity(capacityValue)
		commSoldBuilder.Resizable(false)
		commSoldBuilder.Key(kubeNamespace.UID)

		commSold, err := commSoldBuilder.Create()
		if err != nil {
			glog.Errorf("%s : Failed to build commodity sold: %s", kubeNamespace.Name, err)
			continue
		}
		resourceCommoditiesSold = append(resourceCommoditiesSold, commSold)
	}
	return resourceCommoditiesSold, nil
}

func (builder *namespaceEntityDTOBuilder) getCommoditiesBought(kubeNamespace *repository.KubeNamespace) ([]*proto.CommodityDTO, error) {
	var commoditiesBought []*proto.CommodityDTO
	for resourceType, resource := range kubeNamespace.AllocationResources {
		commodityType, exist := rTypeMapping[resourceType]
		if !exist {
			glog.Errorf("ResourceType %s is not supported", resourceType)
			continue
		}
		commBoughtBuilder := sdkbuilder.NewCommodityDTOBuilder(commodityType)
		commBoughtBuilder.Used(resource.Used)
		commBoughtBuilder.Peak(resource.Used)
		commBoughtBuilder.Key(kubeNamespace.ClusterName)
		commBought, err := commBoughtBuilder.Create()
		if err != nil {
			glog.Errorf("%s: Failed to build commodity bought %s: %s", kubeNamespace.Name, commodityType, err)
			continue
		}
		commoditiesBought = append(commoditiesBought, commBought)
	}
	return commoditiesBought, nil
}
