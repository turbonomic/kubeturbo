// Package compliance parses different compliance rules in Kubernetes, creates commodityDTOs and
// then updates corresponding entityDTOs.
package compliance

import (
	"errors"
	"fmt"
	"sync"

	"github.com/golang/glog"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// ComplianceProcessor provides methods to quickly find an entityDTO and updating its fields, such as commodities bought
// and commodities sold based on different compliance rules.
type ComplianceProcessor struct {
	entityMaps map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO
	sync.RWMutex
}

func NewComplianceProcessor() *ComplianceProcessor {
	return &ComplianceProcessor{
		entityMaps: make(map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO),
	}
}

// Group the given entityDTOs based on entityTypes.
func (cp *ComplianceProcessor) GroupEntityDTOs(entityDTOs []*proto.EntityDTO) {
	cp.Lock()
	defer cp.Unlock()
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
	cp.RLock()
	defer cp.RUnlock()
	for eType := range cp.entityMaps {
		for _, entityDTO := range cp.entityMaps[eType] {
			entityDTOs = append(entityDTOs, entityDTO)
		}
	}
	return entityDTOs
}

// Get one specific entityDTO based on given entity type and entity ID.
func (cp *ComplianceProcessor) GetEntityDTO(eType proto.EntityDTO_EntityType, entityID string) (*proto.EntityDTO, error) {
	cp.RLock()
	defer cp.RUnlock()
	if _, exist := cp.entityMaps[eType]; exist {
		if _, found := cp.entityMaps[eType][entityID]; found {
			return cp.entityMaps[eType][entityID], nil
		} else {
			return nil, fmt.Errorf("given entity ID %s does not exist", entityID)
		}
	} else {
		return nil, fmt.Errorf("given entity type %s does not exist", eType)
	}
}

// Update the entry in the grouped entityDTOs stored in compliance processor.
func (cp *ComplianceProcessor) UpdateEntityDTO(entityDTO *proto.EntityDTO) error {
	eType := entityDTO.GetEntityType()
	cp.Lock()
	defer cp.Unlock()
	if _, exist := cp.entityMaps[eType]; !exist {
		return fmt.Errorf("given entity type %s does not exist", eType)
	}
	eId := entityDTO.GetId()
	if _, exist := cp.entityMaps[eType][eId]; !exist {
		return fmt.Errorf("given entity id %s does not exist", eId)
	}
	cp.entityMaps[eType][eId] = entityDTO
	return nil
}

// insert the given commodity to a given entityDTO.
func (cp *ComplianceProcessor) AddCommoditiesSold(entityDTO *proto.EntityDTO, commodities ...*proto.CommodityDTO) error {
	if entityDTO == nil {
		return errors.New("invalid input: entityDTO is nil")
	}
	if commodities == nil {
		return errors.New("invalid input: commodity is nil")
	}
	commoditiesSold := entityDTO.GetCommoditiesSold()
	for _, comm := range commodities {
		if !hasCommodity(commoditiesSold, comm) {
			commoditiesSold = append(commoditiesSold, comm)
		} else {
			glog.V(4).Infof("Access commodity sold exists. Skip adding access commodity: %v.", comm)
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
		return errors.New("invalid input: entityDTO is nil")
	}
	if commodities == nil {
		return errors.New("invalid input: commodit is nil")
	}
	if provider == nil {
		return errors.New("invalid input: provider is nil")
	}
	foundProvider := false
	for _, commsBoughtFromOneProvider := range entityDTO.GetCommoditiesBought() {
		if commsBoughtFromOneProvider.GetProviderId() == provider.GetId() {
			commsBought := commsBoughtFromOneProvider.GetBought()
			for _, comm := range commodities {
				if !hasCommodity(commsBought, comm) {
					commsBought = append(commsBought, comm)
				} else {
					glog.V(4).Infof("Access commodity bought exists. Skip adding access commodity: %v.", comm)
				}
			}
			commsBoughtFromOneProvider.Bought = commsBought
			foundProvider = true
		}
	}
	if !foundProvider {
		providerID := provider.GetId()
		pType := provider.GetProviderType()
		boughtFromProvider := &proto.EntityDTO_CommodityBought{
			ProviderId:   &providerID,
			ProviderType: &pType,
			Bought:       []*proto.CommodityDTO{},
		}
		boughtFromProvider.Bought = append(boughtFromProvider.Bought, commodities...)
		entityDTO.CommoditiesBought = append(entityDTO.GetCommoditiesBought(), boughtFromProvider)

		err := cp.UpdateEntityDTO(entityDTO)
		if err != nil {
			return fmt.Errorf("failed to update pod entityDTO: %s", err)
		}
	}
	return nil
}

// check for commodity to exist only once.
func hasCommodityUnique(commodities []*proto.CommodityDTO, commDTO *proto.CommodityDTO) bool {
	timesFound := 0
	found := false
	for _, comm := range commodities {
		if commDTO == comm || commDTO.GetCommodityType() == comm.GetCommodityType() &&
			commDTO.GetKey() == comm.GetKey() {
			found = true
			timesFound += 1
		}
	}
	return found && timesFound == 1
}

func hasCommodity(commodityDTOs []*proto.CommodityDTO, commDTO *proto.CommodityDTO) bool {
	for _, comm := range commodityDTOs {
		if commDTO == comm || commDTO.GetCommodityType() == comm.GetCommodityType() &&
			commDTO.GetKey() == comm.GetKey() {
			return true
		}
	}
	return false
}
