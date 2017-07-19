// Package compliance parses different compliance rules in Kubernetes, creates commodityDTOs and
// then updates corresponding entityDTOs.
package compliance

import (
	"errors"
	"fmt"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// ComplianceProcessor provides methods to quickly find an entityDTO and updating its fields, such as commodities bought
// and commodities sold based on different compliance rules.
type ComplianceProcessor struct {
	entityMaps map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO
}

func NewComplianceProcessor() *ComplianceProcessor {
	return &ComplianceProcessor{
		entityMaps: make(map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO),
	}
}

// Group the given entityDTOs based on entityTypes.
func (cp *ComplianceProcessor) GroupEntityDTOs(entityDTOs []*proto.EntityDTO) {
	for _, e := range entityDTOs {
		eType := e.GetEntityType()
		if _, exist := cp.entityMaps[eType]; !exist {
			cp.entityMaps[eType] = make(map[string]*proto.EntityDTO)
		}
		cp.entityMaps[eType][e.GetId()] = e
	}
}

// Get all the entityDTOs from the compliance process.
func (cp *ComplianceProcessor) GetAllEntityDTOs() []*proto.EntityDTO {
	var entityDTOs []*proto.EntityDTO
	for eType := range cp.entityMaps {
		for _, entityDTO := range cp.entityMaps[eType] {
			entityDTOs = append(entityDTOs, entityDTO)
		}
	}
	return entityDTOs
}

// Get one specific entityDTO based on given entity type and entity ID.
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

// Update the entry in the grouped entityDTOs stored in compliance processor.
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
		return errors.New("invalid input: commodity is nil.")
	}
	commoditiesSold := entityDTO.GetCommoditiesSold()
	for _, comm := range commodities {
		if !hasCommoditySold(entityDTO, comm) {
			commoditiesSold = append(commoditiesSold, comm)
		}
	}

	entityDTO.CommoditiesSold = commoditiesSold
	err := cp.UpdateEntityDTO(entityDTO)
	if err != nil {
		return fmt.Errorf("failed to update node entityDTO: %s", err)
	}
	return nil
}

// Add a list of commodityDTOs bought from the given provider to the entityDTO.
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
			for _, comm := range commBoughtType.GetBought() {
				if !hasCommodityBought(commBoughtType, comm) {
					commBoughtType.Bought = append(commBoughtType.GetBought(), commodities...)
				}
			}
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

		err := cp.UpdateEntityDTO(entityDTO)
		if err != nil {
			return fmt.Errorf("failed to update node entityDTO: %s", err)
		}
	}
	return nil
}

// check if a commodity has already been sold by the entity.
func hasCommoditySold(entityDTO *proto.EntityDTO, commDTO *proto.CommodityDTO) bool {
	commoditiesSold := entityDTO.GetCommoditiesSold()
	return hasCommodity(commoditiesSold, commDTO)
}

// check if a commodity has already been bought by the entity
func hasCommodityBought(commodityBought *proto.EntityDTO_CommodityBought, commDTO *proto.CommodityDTO) bool {
	commoditiesBought := commodityBought.GetBought()
	return hasCommodity(commoditiesBought, commDTO)
}

func hasCommodity(commodityDTOs []*proto.CommodityDTO, commDTO *proto.CommodityDTO) bool {
	for _, commSold := range commodityDTOs {
		if commDTO == commSold || commDTO.GetCommodityType() == commSold.GetCommodityType() &&
			commDTO.GetKey() == commSold.GetKey() {
			return true
		}
	}
	return false
}
