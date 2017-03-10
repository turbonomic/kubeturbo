package builder

import (
	"fmt"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type ProviderDTO struct {
	providerType *proto.EntityDTO_EntityType
	Id           string
}

func CreateProvider(pType proto.EntityDTO_EntityType, id string) *ProviderDTO {
	return &ProviderDTO{
		providerType: &pType,
		Id:           id,
	}
}

func (pDto *ProviderDTO) getProviderType() *proto.EntityDTO_EntityType {
	return pDto.providerType
}

func (pDto *ProviderDTO) getId() string {
	return pDto.Id
}

type EntityDTOBuilder struct {
	entityType                   *proto.EntityDTO_EntityType
	id                           *string
	displayName                  *string
	commoditiesSold              []*proto.CommodityDTO
	commoditiesBoughtProviderMap map[string][]*proto.CommodityDTO
	underlying                   []string
	entityProperties             []*proto.EntityDTO_EntityProperty
	origin                       *proto.EntityDTO_EntityOrigin
	replacementEntityData        *proto.EntityDTO_ReplacementEntityMetaData
	monitored                    *bool
	powerState                   *proto.EntityDTO_PowerState
	consumerPolicy               *proto.EntityDTO_ConsumerPolicy
	providerPolicy               *proto.EntityDTO_ProviderPolicy
	ownedBy                      *string
	notification                 []*proto.NotificationDTO
	storageData                  *proto.EntityDTO_StorageData
	diskArrayData                *proto.EntityDTO_DiskArrayData
	applicationData              *proto.EntityDTO_ApplicationData
	virtualMachineData           *proto.EntityDTO_VirtualMachineData
	physicalMachineData          *proto.EntityDTO_PhysicalMachineData
	virtualDataCenterData        *proto.EntityDTO_VirtualDatacenterData
	virtualMachineRelatedData    *proto.EntityDTO_VirtualMachineRelatedData
	physicalMachineRelatedData   *proto.EntityDTO_PhysicalMachineRelatedData
	storageControllerRelatedData *proto.EntityDTO_StorageControllerRelatedData

	currentProvider  *ProviderDTO
	entityDataHasSet bool

	err error
}

func NewEntityDTOBuilder(eType proto.EntityDTO_EntityType, id string) *EntityDTOBuilder {
	return &EntityDTOBuilder{
		entityType: &eType,
		id:         &id,
	}
}

func (eb *EntityDTOBuilder) Create() (*proto.EntityDTO, error) {
	if eb.err != nil {
		return nil, eb.err
	}

	entityDTO := &proto.EntityDTO{
		EntityType:            eb.entityType,
		Id:                    eb.id,
		DisplayName:           eb.displayName,
		CommoditiesSold:       eb.commoditiesSold,
		CommoditiesBought:     buildCommodityBoughtFromMap(eb.commoditiesBoughtProviderMap),
		Underlying:            eb.underlying,
		EntityProperties:      eb.entityProperties,
		Origin:                eb.origin,
		ReplacementEntityData: eb.replacementEntityData,
		Monitored:             eb.monitored,
		PowerState:            eb.powerState,
		ConsumerPolicy:        eb.consumerPolicy,
		ProviderPolicy:        eb.providerPolicy,
		OwnedBy:               eb.ownedBy,
		Notification:          eb.notification,
	}
	if eb.storageData != nil {
		entityDTO.EntityData = &proto.EntityDTO_StorageData_{eb.storageData}
	} else if eb.diskArrayData != nil {
		entityDTO.EntityData = &proto.EntityDTO_DiskArrayData_{eb.diskArrayData}
	} else if eb.applicationData != nil {
		entityDTO.EntityData = &proto.EntityDTO_ApplicationData_{eb.applicationData}
	} else if eb.virtualMachineData != nil {
		entityDTO.EntityData = &proto.EntityDTO_VirtualMachineData_{eb.virtualMachineData}
	} else if eb.physicalMachineData != nil {
		entityDTO.EntityData = &proto.EntityDTO_PhysicalMachineData_{eb.physicalMachineData}
	} else if eb.virtualDataCenterData != nil {
		entityDTO.EntityData = &proto.EntityDTO_VirtualDatacenterData_{eb.virtualDataCenterData}
	}

	if eb.virtualMachineRelatedData != nil {
		entityDTO.RelatedEntityData = &proto.EntityDTO_VirtualMachineRelatedData_{eb.virtualMachineRelatedData}
	} else if eb.physicalMachineRelatedData != nil {
		entityDTO.RelatedEntityData = &proto.EntityDTO_PhysicalMachineRelatedData_{eb.physicalMachineRelatedData}
	} else if eb.storageControllerRelatedData != nil {
		entityDTO.RelatedEntityData = &proto.EntityDTO_StorageControllerRelatedData_{eb.storageControllerRelatedData}
	}

	return entityDTO, nil
}

func (eb *EntityDTOBuilder) DisplayName(displayName string) *EntityDTOBuilder {
	if eb.err != nil {
		return eb
	}
	eb.displayName = &displayName
	return eb
}

// Add a list of commodities to entity commodities sold list.
func (eb *EntityDTOBuilder) SellsCommodities(commDTOs []*proto.CommodityDTO) *EntityDTOBuilder {
	if eb.err != nil {
		return eb
	}
	eb.commoditiesSold = append(eb.commoditiesSold, commDTOs...)
	return eb
}

// Add a single commodity to entity commodities sold list.
func (eb *EntityDTOBuilder) SellsCommodity(commDTO *proto.CommodityDTO) *EntityDTOBuilder {
	if eb.err != nil {
		return eb
	}
	if eb.commoditiesSold == nil {
		eb.commoditiesSold = []*proto.CommodityDTO{}
	}
	eb.commoditiesSold = append(eb.commoditiesSold, commDTO)
	return eb
}

// Set the current provider with provided entity type and ID.
func (eb *EntityDTOBuilder) Provider(provider *ProviderDTO) *EntityDTOBuilder {
	if eb.err != nil {
		return eb
	}
	eb.currentProvider = provider
	return eb
}

// entity buys a list of commodities.
func (eb *EntityDTOBuilder) BuysCommodities(commDTOs []*proto.CommodityDTO) *EntityDTOBuilder {
	if eb.err != nil {
		return eb
	}
	if eb.currentProvider == nil {
		eb.err = fmt.Errorf("Porvider has not been set for current list of commodities: %++v", commDTOs)
		return eb
	}
	for _, commDTO := range commDTOs {
		eb.BuysCommodity(commDTO)
	}
	return eb
}

// entity buys a single commodity
func (eb *EntityDTOBuilder) BuysCommodity(commDTO *proto.CommodityDTO) *EntityDTOBuilder {
	if eb.err != nil {
		return eb
	}
	if eb.currentProvider == nil {
		eb.err = fmt.Errorf("Porvider has not been set for %++v", commDTO)
		return eb
	}

	if eb.commoditiesBoughtProviderMap == nil {
		eb.commoditiesBoughtProviderMap = make(map[string][]*proto.CommodityDTO)
	}

	// add commodity bought to map
	commoditiesSoldByCurrentProvider, exist := eb.commoditiesBoughtProviderMap[eb.currentProvider.Id]
	if !exist {
		commoditiesSoldByCurrentProvider = []*proto.CommodityDTO{}
	}
	commoditiesSoldByCurrentProvider = append(commoditiesSoldByCurrentProvider, commDTO)
	eb.commoditiesBoughtProviderMap[eb.currentProvider.Id] = commoditiesSoldByCurrentProvider

	return eb
}

// Add a single property to entity
func (eb *EntityDTOBuilder) WithProperty(property *proto.EntityDTO_EntityProperty) *EntityDTOBuilder {
	if eb.err != nil {
		return eb
	}

	if eb.entityProperties == nil {
		eb.entityProperties = []*proto.EntityDTO_EntityProperty{}
	}
	// add the property to list.
	eb.entityProperties = append(eb.entityProperties, property)

	return eb
}

// Add multiple properties to entity
func (eb *EntityDTOBuilder) WithProperties(properties []*proto.EntityDTO_EntityProperty) *EntityDTOBuilder {
	if eb.err != nil {
		return eb
	}

	if eb.entityProperties == nil {
		eb.entityProperties = []*proto.EntityDTO_EntityProperty{}
	}
	// add the property to list.
	eb.entityProperties = append(eb.entityProperties, properties...)

	return eb
}

// Set the ReplacementEntityMetadata that will contain the information about the external entity
// that this entity will patch with the metrics data it collected.
func (eb *EntityDTOBuilder) ReplacedBy(replacementEntityMetaData *proto.EntityDTO_ReplacementEntityMetaData) *EntityDTOBuilder {
	if eb.err != nil {
		return eb
	}
	origin := proto.EntityDTO_PROXY
	eb.origin = &origin
	eb.replacementEntityData = replacementEntityMetaData
	return eb
}

func (eb *EntityDTOBuilder) WithPowerState(state proto.EntityDTO_PowerState) *EntityDTOBuilder {
	if eb.err != nil {
		return eb
	}
	eb.powerState = &state
	return eb
}

func (eb *EntityDTOBuilder) Monitored(monitored bool) *EntityDTOBuilder {
	if eb.err != nil {
		return eb
	}
	eb.monitored = &monitored
	return eb
}

func (eb *EntityDTOBuilder) ApplicationData(appData *proto.EntityDTO_ApplicationData) *EntityDTOBuilder {
	if eb.err != nil {
		return eb
	}
	if eb.entityDataHasSet {
		eb.err = fmt.Errorf("EntityData has already been set. Cannot use %v as entity data.", appData)

		return eb
	}
	eb.applicationData = appData
	eb.entityDataHasSet = true
	return eb
}

func (eb *EntityDTOBuilder) VirtualMachineData(vmData *proto.EntityDTO_VirtualMachineData) *EntityDTOBuilder {
	if eb.err != nil {
		return eb
	}
	if eb.entityDataHasSet {
		eb.err = fmt.Errorf("EntityData has already been set. Cannot use %v as entity data.", vmData)

		return eb
	}
	eb.virtualMachineData = vmData
	eb.entityDataHasSet = true
	return eb
}

func buildCommodityBoughtFromMap(providerCommoditiesMap map[string][]*proto.CommodityDTO) []*proto.EntityDTO_CommodityBought {
	if len(providerCommoditiesMap) == 0 {
		return nil
	}
	var commoditiesBought []*proto.EntityDTO_CommodityBought
	for providerId, commodities := range providerCommoditiesMap {
		commoditiesBought = append(commoditiesBought, &proto.EntityDTO_CommodityBought{
			ProviderId: &providerId,
			Bought:     commodities,
		})
	}
	return commoditiesBought
}
