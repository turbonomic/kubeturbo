package dtofactory

import (
	api "k8s.io/api/core/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
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
			glog.Errorf("Error creating commoditiesSold for volume %s: %s", displayName, err)
		}

		entityDTOBuilder.SellsCommodities(commoditiesSold)

		var vols []*api.PersistentVolume
		vols = append(vols, vol)
		volStitchingMgr := stitching.NewVolumeStitchingManager()
		err = volStitchingMgr.ProcessVolumes(vols)
		if err != nil {
			glog.Errorf("Error generating stitching metadata for volume %s: %s", displayName, err)
		} else {
			// reconciliation meta data
			metaData, err := volStitchingMgr.GenerateReconciliationMetaData()
			if err != nil {
				glog.Errorf("Failed to build reconciliation metadata for volume %s: %s", displayName, err)
			}
			entityDTOBuilder = entityDTOBuilder.ReplacedBy(metaData)
		}

		// entities' properties.
		properties := builder.getVolumeProperties(vol, volStitchingMgr)
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

		commBuilder := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_STORAGE_AMOUNT)
		commBuilder.Used(used)
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
func (builder *volumeEntityDTOBuilder) getVolumeProperties(vol *api.PersistentVolume, stitchingMgr *stitching.VolumeStitchingManager) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty

	volProperty := property.BuildVolumeProperties(vol)
	properties = append(properties, volProperty)

	// stitching property.
	isForReconcile := true
	stitchingProperty, err := stitchingMgr.BuildDTOProperty(isForReconcile)
	if err != nil {
		glog.Errorf("failed to build stitching properties for volume %s: %s", vol.Name, err)
	} else {
		glog.V(4).Infof("Volume %s will be reconciled with %s: %s", vol.Name, *stitchingProperty.Name,
			*stitchingProperty.Value)
	}
	properties = append(properties, stitchingProperty)

	return properties
}
