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
	proxyVolumeUUID = "Proxy_Volume_UUID"

	awsVolPrefix   = "aws://"
	azureVolPrefix = "azure://"
	awsVolFormat   = "aws::%v::VL::%v"
	azureVolFormat = "azure::VL::%v"
)

type VolumeStitchingManager struct {
	stitchingUuids string
}

func NewVolumeStitchingManager() *VolumeStitchingManager {
	return &VolumeStitchingManager{}
}

func (s *VolumeStitchingManager) ProcessVolumes(vols []*api.PersistentVolume) error {
	uuids := []string{}
	errorStrings := ""
	atLeastOneProcessed := false
	for _, vol := range vols {
		var uuidGetter VolumeUUIDGetter
		switch {
		case vol.Spec.AWSElasticBlockStore != nil:
			uuidGetter = &awsVolumeUUIDGetter{}
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
	return nil
}

// Get the property name based on whether it is a stitching or reconciliation.
func (s *VolumeStitchingManager) getPropertyName(isForReconcile bool) string {
	if isForReconcile {
		return proxyVolumeUUID
	}
	return supplychain.SUPPLY_CHAIN_CONSTANT_UUID
}

func (s *VolumeStitchingManager) BuildDTOProperty(isForReconcile bool) (*proto.EntityDTO_EntityProperty, error) {
	propertyNamespace := DefaultPropertyNamespace
	propertyName := s.getPropertyName(isForReconcile)

	return &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &propertyName,
		Value:     &s.stitchingUuids,
	}, nil
}

// Create the meta data that will be used during the reconciliation process.
func (s *VolumeStitchingManager) GenerateReconciliationMetaData() (*proto.EntityDTO_ReplacementEntityMetaData, error) {
	replacementEntityMetaDataBuilder := builder.NewReplacementEntityMetaDataBuilder()

	entity := proto.EntityDTO_VIRTUAL_VOLUME
	// TODO use a constant, also find why is this different from supplychain.SUPPLY_CHAIN_CONSTANT_UUID
	attribute := "Uuid"

	propertyDef := &proto.ServerEntityPropDef{
		Entity:    &entity,
		Attribute: &attribute,
	}
	replacementEntityMetaDataBuilder.Matching(proxyVolumeUUID).MatchingExternal(propertyDef)

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
