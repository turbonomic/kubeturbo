package probe

import (
	"github.ibm.com/turbonomic/turbo-api/pkg/api"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// Abstraction for the TurboTarget object in the client
type TurboTargetInfo struct {
	// Category of the target, such as Hypervisor, Storage, etc
	targetCategory string
	// Type of the target, such as Kubernetes, vCenter, etc
	targetType string
	// Display type of the target (subcategory like RedHatOpenShift).
	targetSubType string
	// The field that uniquely identifies the target.
	targetIdentifierField string
	// Account values, such as username, password, nameOrAddress, etc
	// NOTE, it should be consisted with the AccountDefEntry has been defined in registration client.
	accountValues []*proto.AccountValue
}

func (targetInfo *TurboTargetInfo) TargetType() string {
	return targetInfo.targetType
}

func (targetInfo *TurboTargetInfo) TargetSubType() string {
	return targetInfo.targetSubType
}

func (targetInfo *TurboTargetInfo) TargetCategory() string {
	return targetInfo.targetCategory
}

func (targetInfo *TurboTargetInfo) TargetIdentifierField() string {
	return targetInfo.targetIdentifierField
}

func (targetInfo *TurboTargetInfo) AccountValues() []*proto.AccountValue {
	return targetInfo.accountValues
}

func (targetInfo *TurboTargetInfo) String() string {
	return targetInfo.targetCategory + " " + targetInfo.targetType +
		" [" + targetInfo.targetIdentifierField + "]"
}

// Build an API target instance based on information from TurboTargetInfo.
func (targetInfo *TurboTargetInfo) GetTargetInstance() *api.Target {
	var inputFields []*api.InputField
	for _, acctValue := range targetInfo.accountValues {
		inputFields = append(inputFields,
			&api.InputField{
				Name:            acctValue.GetKey(),
				Value:           acctValue.GetStringValue(),
				GroupProperties: []*api.List{},
			})
	}
	return &api.Target{
		Category:    targetInfo.targetCategory,
		Type:        targetInfo.targetType,
		InputFields: inputFields,
	}
}

// Always use a TurboTargetInfoBuilder to create TurboTargetInfo instance.
type TurboTargetInfoBuilder struct {
	targetCategory        string
	targetType            string
	targetSubType         string
	targetIdentifierField string
	accountValues         []*proto.AccountValue
}

func NewTurboTargetInfoBuilder(category, targetType, identifierField string,
	acctValues []*proto.AccountValue) *TurboTargetInfoBuilder {
	return NewTurboTargetInfoBuilderWithSubType(category, targetType, "", identifierField, acctValues)
}

func NewTurboTargetInfoBuilderWithSubType(category, targetType, targetSubType, identifierField string,
	acctValues []*proto.AccountValue) *TurboTargetInfoBuilder {
	return &TurboTargetInfoBuilder{
		targetCategory:        category,
		targetType:            targetType,
		targetSubType:         targetSubType,
		targetIdentifierField: identifierField,
		accountValues:         acctValues,
	}
}

func (builder *TurboTargetInfoBuilder) Create() *TurboTargetInfo {
	return &TurboTargetInfo{
		targetCategory:        builder.targetCategory,
		targetType:            builder.targetType,
		targetSubType:         builder.targetSubType,
		targetIdentifierField: builder.targetIdentifierField,
		accountValues:         builder.accountValues,
	}
}
