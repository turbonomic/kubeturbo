package stitching

import (
	"fmt"
	"strings"

	api "k8s.io/api/core/v1"

	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/supplychain"

	"github.com/golang/glog"
)

/** Note on volume stitching
 ** The uuids are picked from the volume resource fields as listed below:
 ** These are mapped to the cloud specific entity id discovered from specific infra probe.
 ** Azure:
 ** k8s -> vol.Spec.AzureDisk.DataDiskURI
 ** format -> replace `/` with `::`
 **
 ** AWS:
 ** k8s -> vol.Spec.AWSElasticBlockStore.VolumeID
 ** format -> "aws::%v::VL::%v"
 **
 ** Vsphere:
 ** k8s -> vol.Spec.VsphereVolume.VolumePath
 ** format -> as is
 ** Note: we haven't seen a tanzu env yet
 **
 ** CSI:
 ** Decipher aws or azure based on the provider specific strings in driver name
 ** pick vol.Spec.CSI.VolumeHandle and format it according to AWS or azure stitching
 ** uuid format as described above.
**/

const (
	awsVolPrefix   = "aws://"
	azureVolPrefix = "azure://"
	awsVolFormat   = "aws::%v::VL::%v"

	path            = "path"
	proxyVolumeUUID = "Proxy_Volume_UUID"
	volumeId        = "Volume_ID"
)

type VolumeStitchingManager struct {
	stitchingUuids  string
	stitchingPaths  string
	stitchingVolIds string
}

func NewVolumeStitchingManager() *VolumeStitchingManager {
	return &VolumeStitchingManager{}
}

func (s *VolumeStitchingManager) ProcessVolumes(vols []*api.PersistentVolume) error {
	uuids, paths, volIds := []string{}, []string{}, []string{}
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
		case vol.Spec.CSI != nil:
			uuidGetter = &csiVolumeUUIDGetter{}
		default:
			uuidGetter = &defaultVolumeUUIDGetter{}
		}
		uuid, volId, err := uuidGetter.GetVolumeUUID(vol)
		if err != nil {
			// skip this volume
			glog.Errorf("Error processing volume: %v", err)
			errorStrings = fmt.Sprintf("%s : %s", errorStrings, err.Error())
		} else {
			uuids = append(uuids, uuid)
			volIds = append(volIds, volId)
			atLeastOneProcessed = true
		}
	}

	if !atLeastOneProcessed {
		return fmt.Errorf(errorStrings)
	}
	s.stitchingUuids = strings.Join(uuids, ",")
	s.stitchingPaths = strings.Join(paths, ",")
	s.stitchingVolIds = strings.Join(volIds, ",")
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
	if s.stitchingVolIds != "" {
		properties = append(properties, volumeId)
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
		} else if propertyName == volumeId {
			entityProperties = append(entityProperties,
				&proto.EntityDTO_EntityProperty{
					Namespace: &propertyNamespace,
					Name:      &propertyName,
					Value:     &s.stitchingVolIds,
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

	if s.stitchingVolIds != "" {
		attr3 := volumeId
		extPropertyDef3 := &proto.ServerEntityPropDef{
			Entity:    &entity,
			Attribute: &attr3,
		}
		replacementEntityMetaDataBuilder.Matching(volumeId).MatchingExternal(extPropertyDef3)
	}

	usedAndCapacityAndPeakPropertyNames := []string{builder.PropertyCapacity, builder.PropertyUsed, builder.PropertyPeak}
	replacementEntityMetaDataBuilder.PatchSellingWithProperty(proto.CommodityDTO_STORAGE_AMOUNT, usedAndCapacityAndPeakPropertyNames)
	meta := replacementEntityMetaDataBuilder.Build()
	return meta, nil
}

type VolumeUUIDGetter interface {
	GetVolumeUUID(vol *api.PersistentVolume) (string, string, error)
	Name() string
}

type defaultVolumeUUIDGetter struct {
}

func (d *defaultVolumeUUIDGetter) Name() string {
	return "Default"
}

func (d *defaultVolumeUUIDGetter) GetVolumeUUID(vol *api.PersistentVolume) (string, string, error) {
	// TODO: Find a common id that suits most providers
	uid := string(vol.UID)
	if len(uid) < 1 {
		return "", "", fmt.Errorf("vol uid is empty: %v", vol.Name)
	}

	return uid, "", nil
}

type awsVolumeUUIDGetter struct {
}

func (aws *awsVolumeUUIDGetter) Name() string {
	return "AWS"
}

func (aws *awsVolumeUUIDGetter) GetVolumeUUID(vol *api.PersistentVolume) (string, string, error) {
	if vol.Spec.AWSElasticBlockStore == nil {
		return "", "", fmt.Errorf("not a valid AWS provisioned volume: %v", vol.Name)
	}

	return extractAWSVolumeUuid(vol.Spec.AWSElasticBlockStore.VolumeID, vol)
}

func extractAWSVolumeUuid(volID string, vol *api.PersistentVolume) (string, string, error) {
	volName := vol.Name
	//1. split the suffix into two parts:
	// aws://us-east-2c/vol-0e4eaa3ef79bcb5a9 -> [us-east-2c, vol-0e4eaa3ef79bcb5a9]
	suffix := volID
	if strings.HasPrefix(volID, awsVolPrefix) {
		suffix = volID[len(awsVolPrefix):]
	}

	parts := strings.Split(suffix, "/")
	if len(parts) < 2 {
		// Try to extract the zone information from the volume labels if they exist
		// "topology.kubernetes.io/region": "us-east-2"
		// "topology.kubernetes.io/zone": "us-east-2c"
		if vol.Labels == nil || len(vol.Labels) < 1 {
			if strings.HasPrefix(suffix, "vol-") {
				// If region label do not exist, return volume ID without region.
				// TP will try to stitch with the LocalName property in AWS volume DTOs.
				return suffix, suffix, nil
			} else {
				return "", "", fmt.Errorf("failed to split uuid (%d): %v, for volume %v", len(parts), parts, volName)
			}
		}
		parts = append(parts, parts[0])
		parts[0] = ""
		region := ""
		zone := ""
		for labelKey, labelV := range vol.Labels {
			if strings.HasSuffix(labelKey, "region") {
				region = labelV
			}
			if strings.HasSuffix(labelKey, "zone") {
				zone = labelV
			}
		}
		if zone != "" {
			parts[0] = zone
		} else if region != "" {
			parts[0] = region + "-"
		}
	}

	//2. get region by removing the zone suffix
	if len(parts[0]) < 2 {
		return "", "", fmt.Errorf("invalid zone Id: %v, for volume: %v", volID, volName)
	}
	end := len(parts[0]) - 1
	region := parts[0][0:end]

	//3. aws::us-east-2::VL::vol-0e4eaa3ef79bcb5a9
	return fmt.Sprintf(awsVolFormat, region, parts[1]), parts[1], nil
}

type azureVolumeUUIDGetter struct {
}

func (azure *azureVolumeUUIDGetter) Name() string {
	return "AZURE"
}

func (azure *azureVolumeUUIDGetter) GetVolumeUUID(vol *api.PersistentVolume) (string, string, error) {
	if vol.Spec.AzureDisk == nil {
		return "", "", fmt.Errorf("not a valid Azure provisioned volume: %v", vol.Name)
	}
	// TODO: handle azureFile type if and when we come across a k8s environment
	// which uses that.
	uuid := extractAzureVolumeUuid(vol.Spec.AzureDisk.DataDiskURI)
	return uuid, "", nil
}

func extractAzureVolumeUuid(diskURI string) string {
	//1. Get uuid by replacing the '/' with '::' in the path:
	// /subscriptions/6a5d73a4-e446-4c75-8f18-073b2f60d851/resourceGroups/
	// mc_adveng_aks-virtual_westus/providers/Microsoft.Compute/disks/
	// kubernetes-dynamic-pvc-0a2016c8-095c-481e-800d-684e277234e4
	// ->
	// ::subscriptions::6a5d73a4-e446-4c75-8f18-073b2f60d851::resourcegroups::
	//  mc_adveng_aks-virtual_westus::providers::microsoft.compute::disks::
	//  kubernetes-dynamic-pvc-0a2016c8-095c-481e-800d-684e277234e4
	return strings.ToLower(strings.ReplaceAll(diskURI, "/", "::"))
}

type vsphereVolumeUUIDGetter struct {
}

func (azure *vsphereVolumeUUIDGetter) Name() string {
	return "VSPHERE"
}

func (azure *vsphereVolumeUUIDGetter) GetVolumeUUID(vol *api.PersistentVolume) (string, string, error) {
	if vol.Spec.VsphereVolume == nil {
		return "", "", fmt.Errorf("not a valid Vsphere provisioned volume: %v", vol.Name)
	}

	return string(vol.UID), "", nil
}

type csiVolumeUUIDGetter struct {
}

func (csi *csiVolumeUUIDGetter) Name() string {
	return "CSI"
}

func (csi *csiVolumeUUIDGetter) GetVolumeUUID(vol *api.PersistentVolume) (string, string, error) {
	driverName := strings.ToLower(vol.Spec.CSI.Driver)
	volumeHandle := vol.Spec.CSI.VolumeHandle

	// The csi based volume driver name for aws generally is ebs.csi.aws.com.
	// We will need evidence for this name to be something else in some installation.
	// If we get that then it will become necessary to have this configured with the kuebturbo install.
	if strings.Contains(driverName, "aws") && strings.Contains(driverName, "csi") {
		return extractAWSVolumeUuid(volumeHandle, vol)
	}

	// The csi based volume driver names for azure generally are disk.csi.azure.com.
	// We will need evidence for this name to be something else in some installation.
	// If we get that then it will become necessary to have this configured with the kuebturbo install.
	if strings.Contains(driverName, "azure") && strings.Contains(driverName, "csi") {
		return extractAzureVolumeUuid(volumeHandle), "", nil
	}

	return "", "", fmt.Errorf("unhandled csi driver %s for volume: %s", driverName, vol.Name)
}
