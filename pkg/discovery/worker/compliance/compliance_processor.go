package compliance

import (
	"errors"
	"fmt"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type ComplianceProcessor struct {
	entityMaps map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO
}

func NewComplianceProcessor() *ComplianceProcessor {
	return &ComplianceProcessor{
		entityMaps: make(map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO),
	}
}

func (cp *ComplianceProcessor) GroupEntityDTOs(entityDTOs []*proto.EntityDTO) {
	for _, e := range entityDTOs {
		eType := e.GetEntityType()
		if _, exist := cp.entityMaps[eType]; !exist {
			cp.entityMaps[eType] = make(map[string]*proto.EntityDTO)
		}
		cp.entityMaps[eType][e.GetId()] = e
	}
}

func (cp *ComplianceProcessor) GetAllEntityDTOs() []*proto.EntityDTO {
	var entityDTOs []*proto.EntityDTO
	for eType := range cp.entityMaps {
		for _, entityDTO := range cp.entityMaps[eType] {
			entityDTOs = append(entityDTOs, entityDTO)
		}
	}
	return entityDTOs
}

func (cp *ComplianceProcessor) GetEntityDTO(eType proto.EntityDTO_EntityType, entityID string) (*proto.EntityDTO, error) {
	if _, exist := cp.entityMaps[eType]; exist {
		if _, found := cp.entityMaps[eType][entityID]; found {
			return cp.entityMaps[eType][entityID], nil
		} else {
			return nil, fmt.Errorf("given entity ID %s does not exist.", entityID)
		}
	} else {
		return nil, fmt.Errorf("given entity type %s does not exist.", eType)
	}
}

func (cp *ComplianceProcessor) UpdateEntityDTO(entityDTO *proto.EntityDTO) error {
	eType := entityDTO.GetEntityType()
	if _, exist := cp.entityMaps[eType]; !exist {
		return fmt.Errorf("given entity type %s does not exist.", eType)
	}
	cp.entityMaps[eType][entityDTO.GetId()] = entityDTO
	return nil
}

// insert the given commodity to a given entityDTO.
func (cp *ComplianceProcessor) AddCommoditiesSold(entityDTO *proto.EntityDTO, commodities ...*proto.CommodityDTO) error {
	if entityDTO == nil {
		return errors.New("invalid input: entityDTO is nil.")
	}
	if commodities == nil {
		return errors.New("invalid input: commodit is nil.")
	}
	commoditiesSold := entityDTO.GetCommoditiesSold()
	for _, comm := range commodities {
		if !hasCommoditySold(entityDTO, comm) {
			commoditiesSold = append(commoditiesSold, comm)
		}
	}
	entityDTO.CommoditiesSold = commoditiesSold
	return nil
}

func (cp *ComplianceProcessor) AddCommoditiesBought(entityDTO *proto.EntityDTO, provider *sdkbuilder.ProviderDTO, commodities ...*proto.CommodityDTO) error {
	if entityDTO == nil {
		return errors.New("invalid input: entityDTO is nil.")
	}
	if commodities == nil {
		return errors.New("invalid input: commodit is nil.")
	}
	foundProvider := false
	for _, commBoughtType := range entityDTO.GetCommoditiesBought() {
		// TODO compare GetProviderType
		if commBoughtType.GetProviderId() == provider.GetId() { // && commBoughtType.GetProviderType() == provider.GetProviderType() {
			commBoughtType.Bought = append(commBoughtType.GetBought(), commodities...)
			foundProvider = true
		}
	}
	if !foundProvider {
		providerID := provider.GetId()
		pType := provider.GetProviderType()
		boughtType := &proto.EntityDTO_CommodityBought{
			ProviderId:   &providerID,
			ProviderType: &pType,
			Bought:       []*proto.CommodityDTO{},
		}
		boughtType.Bought = append(boughtType.Bought, commodities...)
		entityDTO.CommoditiesBought = append(entityDTO.GetCommoditiesBought(), boughtType)
	}
	return nil
}

func (cp *ComplianceProcessor) getEntity(entityType proto.EntityDTO_EntityType, entityID string) (*proto.EntityDTO, error) {
	entityMap, exist := cp.entityMaps[entityType]
	if !exist {
		return nil, fmt.Errorf("cannot find entityDTO with entity type %s", entityType)
	}
	entityDTO, exist := entityMap[entityID]
	if !exist {
		return nil, fmt.Errorf("cannot find entityDTO with given ID: %s", entityID)
	}
	return entityDTO, nil
}

func hasCommoditySold(entityDTO *proto.EntityDTO, commDTO *proto.CommodityDTO) bool {
	commoditiesSold := entityDTO.GetCommoditiesSold()
	for _, commSold := range commoditiesSold {
		if commDTO == commSold || commDTO.GetCommodityType() == commSold.GetCommodityType() &&
			commDTO.GetKey() == commSold.GetKey() {
			return true
		}
	}
	return false
}
