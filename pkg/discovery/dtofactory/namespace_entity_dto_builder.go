package dtofactory

import (
	"fmt"
	"math"

	"github.ibm.com/turbonomic/kubeturbo/pkg/util"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/detectors"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	sdkbuilder "github.ibm.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type namespaceEntityDTOBuilder struct {
	ClusterSummary *repository.ClusterSummary
	NamespaceMap   map[string]*repository.KubeNamespace
}

func NewNamespaceEntityDTOBuilder(clusterSummary *repository.ClusterSummary) *namespaceEntityDTOBuilder {
	builder := &namespaceEntityDTOBuilder{
		ClusterSummary: clusterSummary,
		NamespaceMap:   clusterSummary.NamespaceMap,
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
		commoditiesSold, controllable, err := builder.getQuotaCommoditiesSold(namespace)
		if err != nil {
			glog.Errorf("Error creating commoditiesSold for %s: %s", namespace.Name, err)
			continue
		}
		entityDTOBuilder.SellsCommodities(commoditiesSold)

		// When the "getQuotaCommoditiesSold" method returns false for the "controllable" flag,
		// it indicates that we have temporarily resized a namespace's ResourceQuota. As such,
		// we want to avoid the market generating resize actions against the quota, especially
		// within environments where action executaion automated.
		if !controllable {
			entityDTOBuilder.ConsumerPolicy(&proto.EntityDTO_ConsumerPolicy{
				Controllable: &controllable,
			})
		}

		commoditiesBought, err := builder.getCommoditiesBought(namespace)
		if err != nil {
			glog.Errorf("Error creating commoditiesBought for %s: %v", namespace.Name, err)
			continue
		}
		entityDTOBuilder.Provider(sdkbuilder.CreateProvider(proto.EntityDTO_CONTAINER_PLATFORM_CLUSTER, namespace.ClusterName)).BuysCommodities(commoditiesBought)
		// Set movable false to avoid moving Namespace across Clusters
		entityDTOBuilder.IsMovable(proto.EntityDTO_CONTAINER_PLATFORM_CLUSTER, false)
		// also set up the aggregatedBy relationship with the cluster
		entityDTOBuilder.AggregatedBy(namespace.ClusterName)

		// Namespace entity cannot be provisioned or suspended by Turbonomic analysis
		entityDTOBuilder.IsProvisionable(false)
		entityDTOBuilder.IsSuspendable(false)

		// Set the average CPU speed on the NamespaceData
		if namespace.AverageNodeCpuFrequency > 0.0 {
			entityDTOBuilder.NamespaceData(&proto.EntityDTO_NamespaceData{
				AverageNodeCpuFrequency: &namespace.AverageNodeCpuFrequency,
			})
		}

		entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

		// build entityDTO.
		entityDto, err := entityDTOBuilder.WithProperty(property.BuildNamespaceProperty(namespace.Name)).
			WithProperty(property.BuildFullyQualifiedNameProperty(builder.ClusterSummary.Name + util.NamingQualifierSeparator + namespace.Name)).
			WithProperties(property.BuildLabelAnnotationProperties(namespace.Labels, namespace.Annotations, detectors.AWNamespace)).
			Create()
		if err != nil {
			glog.Errorf("Failed to build Namespace entityDTO: %s", err)
			continue
		}

		result = append(result, entityDto)
		glog.V(4).Infof("Namespace DTO: %+v", entityDto)
	}
	return result, nil
}

func (builder *namespaceEntityDTOBuilder) getQuotaCommoditiesSold(kubeNamespace *repository.KubeNamespace) ([]*proto.CommodityDTO, bool, error) {
	var resourceCommoditiesSold []*proto.CommodityDTO
	controllable := true
	for resourceType, resource := range kubeNamespace.AllocationResources {
		cType, exist := rTypeMapping[resourceType]
		if !exist {
			continue
		}
		capacityValue := resource.Capacity
		usedValue := resource.Used

		// Kubeturbo will temporarily resize a Namespace ResourceQuota to accomodate a new pod
		// starting during action execution. When it does this, it adds an annotation to the
		// quota that can be used to identify and revert the temporary resizing later.
		// While a quota is temporarily resized, Kubeturbo will return the original
		// configuration's capacity, NOT the temporarily inflated one. This leads to the
		// usage exceeding the capacity. If it didn't, then there wouldn't been no need to
		// temporarily resize the quota. To avoid generating these quota resize actions,
		// temporarily disable action generation against the namespace/quota when the
		// annotation exists.
		if controllable {
			for _, quota := range kubeNamespace.QuotaList {
				if _, exists := quota.Annotations[util.QuotaAnnotationKey]; exists {
					// Mark the Namespace as uncontrollable so that quota actions aren't generated
					controllable = false
					glog.V(4).Infof("Disabling quota resize actions on Namespace %s where ResourceQuota is resized temporarily to allow successful completion of other actions",
						kubeNamespace.Name)
				}
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
	return resourceCommoditiesSold, controllable, nil
}

func (builder *namespaceEntityDTOBuilder) getCommoditiesBought(kubeNamespace *repository.KubeNamespace) ([]*proto.CommodityDTO, error) {
	clusterKey := GetClusterKey(kubeNamespace.ClusterName)
	clusterCommBought, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_CLUSTER).
		Key(clusterKey).Used(1).Create()
	if err != nil {
		glog.Errorf("Failed to build cluster commodity bought with key %s: %s", kubeNamespace.Name, err)
		return nil, err
	}
	commoditiesBought := []*proto.CommodityDTO{clusterCommBought}
	for resourceType, resource := range kubeNamespace.ComputeResources {
		commBought, err := builder.getCommodityBought(resourceType, resource)
		if err != nil {
			glog.Errorf("%s: Failed to build commodity bought with resource type %s: %s", kubeNamespace.Name,
				resourceType, err)
			continue
		}
		commoditiesBought = append(commoditiesBought, commBought)
	}
	return commoditiesBought, nil
}

func (builder *namespaceEntityDTOBuilder) getCommodityBought(resourceType metrics.ResourceType,
	resource *repository.KubeDiscoveredResource) (*proto.CommodityDTO, error) {
	commodityType, exist := rTypeMapping[resourceType]
	if !exist {
		return nil, fmt.Errorf("resourceType %s is not supported", resourceType)
	}
	commBoughtBuilder := sdkbuilder.NewCommodityDTOBuilder(commodityType)
	used := resource.Used
	peak := resource.Used
	if resource.Points != nil && len(resource.Points) > 0 {
		usedSum := 0.0
		for _, point := range resource.Points {
			peak = math.Max(peak, point.Value)
			usedSum += point.Value
		}
		used = usedSum / float64(len(resource.Points))
	}
	commBoughtBuilder.Used(used)
	commBoughtBuilder.Peak(peak)
	return commBoughtBuilder.Create()
}
