package dtofactory

import (
	"math"

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

		commoditiesSold, cap := builder.getVolumeCommoditiesSold(vol, podVolumes)
		if cap == 0 || len(commoditiesSold) < 1 {
			// We don't add a dangling volume
			continue
		}
		entityDTOBuilder.SellsCommodities(commoditiesSold)

		var vols []*api.PersistentVolume
		vols = append(vols, vol)
		volStitchingMgr := stitching.NewVolumeStitchingManager()
		err := volStitchingMgr.ProcessVolumes(vols)
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

func (builder *volumeEntityDTOBuilder) getVolumeCommoditiesSold(vol *api.PersistentVolume, podVols []repository.PodVolume) ([]*proto.CommodityDTO, float64) {
	var commoditiesSold []*proto.CommodityDTO

	volumeCapacity := float64(0)
	volumeUsed := float64(0)
	for _, podVol := range podVols {
		// Volume capacity metrics is available as part of volume spec.
		// However we don't use that if the actual queried volume size
		// is available and discovered by the kubelet as part of
		// pod metrics for that volume.
		capacity, used, found := builder.getVolumeMetrics(vol, podVol.QualifiedPodName, podVol.MountName)
		if !found {
			// Not adding the capacity value from the volume spec (vol.spec.capacity) here
			// currently ensures that there are no dangling volumes shown in supply chain
			// if the volume is not used by a pod, or if there are no volume metrics discovered
			// in some environment.
			// (TODO): We can revisit this in future. A volume can also be discovered with just
			// the capacity value and not connected to any other entity.
			continue
		}

		// Both capacity and used should ideally result in same values when
		// the metrics is collected from different pods, but we still pick the
		// max for used as the metrics might be coming from multiple pods at a
		// different point in time.
		// We do this rather then adding multiple sold commodities as the server
		// does not allow having multiple sold commodities with the same key (empty here).
		volumeCapacity = capacity
		volumeUsed = math.Max(volumeUsed, used)
	}

	if volumeCapacity != float64(0) {
		commBuilder := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_STORAGE_AMOUNT)
		commBuilder.Used(volumeUsed)
		commBuilder.Capacity(volumeCapacity)
		commodity, err := commBuilder.Create()
		if err != nil {
			glog.Errorf("Error creating commoditySold by volume %s: %v ", vol.Name, err)
		} else {
			// TODO(irfanurrehman): Set resizable depending on node properties
			commoditiesSold = append(commoditiesSold, commodity)
		}
	}

	return commoditiesSold, volumeCapacity
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
