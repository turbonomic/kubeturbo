package stitching

import (
	"fmt"
	"strings"

	api "k8s.io/api/core/v1"

	"github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.com/turbonomic/turbo-go-sdk/pkg/supplychain"

	"github.com/golang/glog"
)

const (
	awsVolPrefix   = "aws://"
	azureVolPrefix = "azure://"
	awsVolFormat   = "aws::%v::VL::%v"
	azureVolFormat = "azure::VL::%v"

	path            = "path"
	proxyVolumeUUID = "Proxy_Volume_UUID"
)

type VolumeStitchingManager struct {
	stitchingUuids string
	stitchingPaths string
}

func NewVolumeStitchingManager() *VolumeStitchingManager {
	return &VolumeStitchingManager{}
}

func (s *VolumeStitchingManager) ProcessVolumes(vols []*api.PersistentVolume) error {
	uuids, paths := []string{}, []string{}
	errorStrings := ""
	atLeastOneProcessed := false
	for _, vol := range vols {
		var uuidGetter VolumeUUIDGetter
		switch {
		case vol.Spec.AWSElasticBlockStore != nil:
			uuidGetter = &awsVolumeUUIDGetter{}
		case vol.Spec.AzureDisk != nil || vol.Spec.AzureFile != nil:
			uuidGetter = &azureVolumeUUIDGetter{}
		case vol.Spec.VsphereVolume != nil:
			// We keep uuid getter for vsphere although not used for stitching
			// in current environments. This will be useful for environments like tanzu.
			// We will need to update the appropriate id when we have a working tanzu cluster.
			// Please check https://rbcommons.com/s/VMTurbo/r/42617/ for path based stitching details.
			uuidGetter = &vsphereVolumeUUIDGetter{}
			paths = append(paths, vol.Spec.VsphereVolume.VolumePath)
			atLeastOneProcessed = true
		default:
			uuidGetter = &defaultVolumeUUIDGetter{}
		}
		uuid, err := uuidGetter.GetVolumeUUID(vol)
		if err != nil {
			// skip this volume
			glog.Errorf("Error processing volume: %v", err)
			errorStrings = fmt.Sprintf("%s : %s", errorStrings, err.Error())
		} else {
			uuids = append(uuids, uuid)
			atLeastOneProcessed = true
		}
	}

	if !atLeastOneProcessed {
		return fmt.Errorf(errorStrings)
	}
	s.stitchingUuids = strings.Join(uuids, ",")
	s.stitchingPaths = strings.Join(paths, ",")
	return nil
}

// Get the property names based on whether it is a stitching or reconciliation.
func (s *VolumeStitchingManager) getPropertyNames(isForReconcile bool) []string {
	properties := []string{}
	if isForReconcile {
		properties = append(properties, proxyVolumeUUID)
	} else {
		properties = append(properties, supplychain.SUPPLY_CHAIN_CONSTANT_UUID)
	}
	if s.stitchingPaths != "" {
		properties = append(properties, path)
	}
	return properties
}

func (s *VolumeStitchingManager) BuildDTOProperties(isForReconcile bool) []*proto.EntityDTO_EntityProperty {
	propertyNamespace := DefaultPropertyNamespace
	propertyNames := s.getPropertyNames(isForReconcile)

	entityProperties := []*proto.EntityDTO_EntityProperty{}
	for _, val := range propertyNames {
		// We report 2 properties in case of vsphere
		propertyName := val
		if propertyName == path {
			entityProperties = append(entityProperties,
				&proto.EntityDTO_EntityProperty{
					Namespace: &propertyNamespace,
					Name:      &propertyName,
					Value:     &s.stitchingPaths,
				})
		} else {
			entityProperties = append(entityProperties,
				&proto.EntityDTO_EntityProperty{
					Namespace: &propertyNamespace,
					Name:      &propertyName,
					Value:     &s.stitchingUuids,
				})
		}
	}
	return entityProperties
}

// Create the meta data that will be used during the reconciliation process.
func (s *VolumeStitchingManager) GenerateReconciliationMetaData() (*proto.EntityDTO_ReplacementEntityMetaData, error) {
	replacementEntityMetaDataBuilder := builder.NewReplacementEntityMetaDataBuilder()
	entity := proto.EntityDTO_VIRTUAL_VOLUME

	attr1 := supplychain.SUPPLY_CHAIN_CONSTANT_UUID
	extPropertyDef1 := &proto.ServerEntityPropDef{
		Entity:    &entity,
		Attribute: &attr1,
	}
	replacementEntityMetaDataBuilder.Matching(proxyVolumeUUID).MatchingExternal(extPropertyDef1)

	if s.stitchingPaths != "" {
		attr2 := path
		extPropertyDef2 := &proto.ServerEntityPropDef{
			Entity:    &entity,
			Attribute: &attr2,
		}
		replacementEntityMetaDataBuilder.Matching(path).MatchingExternal(extPropertyDef2)
	}

	usedAndCapacityAndPeakPropertyNames := []string{builder.PropertyCapacity, builder.PropertyUsed, builder.PropertyPeak}
	replacementEntityMetaDataBuilder.PatchSellingWithProperty(proto.CommodityDTO_STORAGE_AMOUNT, usedAndCapacityAndPeakPropertyNames)
	meta := replacementEntityMetaDataBuilder.Build()
	return meta, nil
}

type VolumeUUIDGetter interface {
	GetVolumeUUID(vol *api.PersistentVolume) (string, error)
	Name() string
}

type defaultVolumeUUIDGetter struct {
}

func (d *defaultVolumeUUIDGetter) Name() string {
	return "Default"
}

func (d *defaultVolumeUUIDGetter) GetVolumeUUID(vol *api.PersistentVolume) (string, error) {
	// TODO: Find a common id that suits most providers
	uid := string(vol.UID)
	if len(uid) < 1 {
		return "", fmt.Errorf("vol uid is empty: %v", vol.Name)
	}

	return uid, nil
}

type awsVolumeUUIDGetter struct {
}

func (aws *awsVolumeUUIDGetter) Name() string {
	return "AWS"
}

func (aws *awsVolumeUUIDGetter) GetVolumeUUID(vol *api.PersistentVolume) (string, error) {
	if vol.Spec.AWSElasticBlockStore == nil {
		return "", fmt.Errorf("not a valid AWS provisioned volume: %v", vol.Name)
	}

	volID := vol.Spec.AWSElasticBlockStore.VolumeID
	//1. split the suffix into two parts:
	// aws://us-east-2c/vol-0e4eaa3ef79bcb5a9 -> [us-east-2c, vol-0e4eaa3ef79bcb5a9]
	suffix := volID[len(awsVolPrefix):]
	parts := strings.Split(suffix, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("failed to split uuid (%d): %v, for volume %v", len(parts), parts, vol.Name)
	}

	//2. get region by removing the zone suffix
	if len(parts[0]) < 2 {
		return "", fmt.Errorf("invalid zone Id: %v, for volume: %v", volID, vol.Name)
	}
	end := len(parts[0]) - 1
	region := parts[0][0:end]

	result := fmt.Sprintf(awsVolFormat, region, parts[1])
	return result, nil
}

type azureVolumeUUIDGetter struct {
}

func (azure *azureVolumeUUIDGetter) Name() string {
	return "AZURE"
}

func (azure *azureVolumeUUIDGetter) GetVolumeUUID(vol *api.PersistentVolume) (string, error) {
	if vol.Spec.AzureDisk == nil {
		return "", fmt.Errorf("not a valid Azure provisioned volume: %v", vol.Name)
	}
	// TODO: handle azureFile type if and when we come across a k8s environment
	// which uses that.

	diskURI := vol.Spec.AzureDisk.DataDiskURI
	//1. Get uuid by replacing the '/' with '::' in the path:
	// /subscriptions/6a5d73a4-e446-4c75-8f18-073b2f60d851/resourceGroups/
	// mc_adveng_aks-virtual_westus/providers/Microsoft.Compute/disks/
	// kubernetes-dynamic-pvc-0a2016c8-095c-481e-800d-684e277234e4
	// ->
	// ::subscriptions::6a5d73a4-e446-4c75-8f18-073b2f60d851::resourceGroups::
	//  mc_adveng_aks-virtual_westus::providers::Microsoft.Compute::disks::
	//  kubernetes-dynamic-pvc-0a2016c8-095c-481e-800d-684e277234e4
	stitchingUUID := strings.ToLower(strings.ReplaceAll(diskURI, "/", "::"))

	return stitchingUUID, nil
}

type vsphereVolumeUUIDGetter struct {
}

func (azure *vsphereVolumeUUIDGetter) Name() string {
	return "VSPHERE"
}

func (azure *vsphereVolumeUUIDGetter) GetVolumeUUID(vol *api.PersistentVolume) (string, error) {
	if vol.Spec.VsphereVolume == nil {
		return "", fmt.Errorf("not a valid Vsphere provisioned volume: %v", vol.Name)
	}

	return string(vol.UID), nil
}
