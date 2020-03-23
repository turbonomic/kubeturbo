package dtofactory

import (
	"fmt"

	api "k8s.io/api/core/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

type volumeEntityDTOBuilder struct {
	podVolumeMetrics []*repository.PodVolumeMetrics
}

func NewVolumeEntityDTOBuilder(podVolumeMetrics []*repository.PodVolumeMetrics) *volumeEntityDTOBuilder {
	return &volumeEntityDTOBuilder{
		podVolumeMetrics: podVolumeMetrics,
	}
}

// Build entityDTOs based on the given volume to pod mappings.
func (builder *volumeEntityDTOBuilder) BuildEntityDTOs(volToPodsMap map[*api.PersistentVolume][]repository.PodVolume) ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO

	for vol, podVolumes := range volToPodsMap {
		volID := string(vol.UID)
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_VIRTUAL_VOLUME, volID)
		displayName := vol.Name
		entityDTOBuilder.DisplayName(displayName)

		commoditiesSold, cap, err := builder.getVolumeCommoditiesSold(vol, podVolumes)
		if err != nil {
			glog.Errorf("Error when create commoditiesSold for volume %s: %s", displayName, err)
		}

		entityDTOBuilder.SellsCommodities(commoditiesSold)

		// entities' properties.
		properties := builder.getVolumeProperties(vol)
		entityDTOBuilder = entityDTOBuilder.WithProperties(properties)

		entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)

		// build entityDTO.
		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build Volume: %s entityDTO: %s", displayName, err)
			continue
		}

		volCapacity := float32(cap)
		isEphemaral := false
		virtualVolumeData := &proto.EntityDTO_VirtualVolumeData{
			StorageAmountCapacity: &volCapacity,
			IsEphemeral:           &isEphemaral,
		}
		entityDto.EntityData = &proto.EntityDTO_VirtualVolumeData_{VirtualVolumeData: virtualVolumeData}

		result = append(result, entityDto)
	}

	return result, nil

}

func (builder *volumeEntityDTOBuilder) getVolumeCommoditiesSold(vol *api.PersistentVolume, podVols []repository.PodVolume) ([]*proto.CommodityDTO, float64, error) {
	var commoditiesSold []*proto.CommodityDTO

	volumeCapacity := float64(0)
	for _, podVol := range podVols {
		podKey := podVol.QualifiedPodName
		mountName := podVol.MountName
		// Volume capacity metrics is available as part of volume spec.
		// However we dont use that if the actual queried volume size
		// is available and discovered by the kubelet as part of
		// pod metrics for that volume. If a volume is not mounted, then
		// we use the capacity listed in volume spec.
		capacity, used, found := builder.getVolumeMetrics(vol, podVol.QualifiedPodName, podVol.MountName)
		if !found {
			continue
		}
		volumeCapacity = capacity

		commKey := fmt.Sprintf("%s/%s", podKey, mountName)
		commBuilder := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_STORAGE_AMOUNT).Key(commKey)
		commBuilder.Used(used)
		commBuilder.Peak(used)
		commBuilder.Capacity(capacity)
		commodityPerPod, err := commBuilder.Create()
		if err != nil {
			return nil, volumeCapacity, err
		}

		// TODO(irfanurrehman): Set resisable depending on node properties

		commoditiesSold = append(commoditiesSold, commodityPerPod)
	}

	return commoditiesSold, volumeCapacity, nil
}

func (builder *volumeEntityDTOBuilder) getVolumeMetrics(vol *api.PersistentVolume, podKey, mountName string) (float64, float64, bool) {
	for _, metricEntry := range builder.podVolumeMetrics {
		if metricEntry.Volume.Name == vol.Name &&
			metricEntry.MountName == mountName &&
			metricEntry.QualifiedPodName == podKey {
			return metricEntry.Capacity, metricEntry.Used, true
		}
	}

	return float64(0), float64(0), false
}

// Get the properties of the volume.
// TODO(irfanurrehman): Add stitching properties.
func (builder *volumeEntityDTOBuilder) getVolumeProperties(vol *api.PersistentVolume) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty

	volProperty := property.BuildVolumeProperties(vol)
	properties = append(properties, volProperty)

	return properties
}
