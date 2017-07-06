package discovery

import (
	"fmt"
	"math"
	"reflect"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

func compareDiscoveryResults(discResFromOldFramework, discResFromNewFramework []*proto.EntityDTO) {

	glog.Infof("Start validation")

	oldGroup := groupByEntityDtoType(discResFromOldFramework)
	newGroup := groupByEntityDtoType(discResFromNewFramework)

	for entityType, entitiesFromOld := range oldGroup {
		glog.Infof("Now examine %s", entityType)
		if entitiesFromNew, exist := newGroup[entityType]; exist {
			delete(newGroup, entityType)

			// compare size
			if len(entitiesFromNew) != len(entitiesFromOld) {
				glog.Errorf("%s check: Discoverd %d from old, but got %d from new.", entityType, len(entitiesFromOld), len(entitiesFromNew))
				//glog.Errorf("old %++v; new %++v", entitiesFromOld, entitiesFromNew)
				showEntityDTOListDifference(entitiesFromOld, entitiesFromNew)
				//continue
			}

			// compare actual entity properties.
			for id, oldEntity := range entitiesFromOld {
				newEntity, found := entitiesFromNew[id]
				if !found {
					glog.Errorf("%s check: Old had %s with id %s, but didn't find in new.", entityType, oldEntity.GetDisplayName(), id)
					continue
				}
				if isClear := compareEntityDTOs(oldEntity, newEntity); isClear {
					glog.Infof("All checks for %s - %s are clear.", entityType, oldEntity.GetDisplayName())
				} else {
					glog.Errorf("Checks failed for old entity %s and new entity %s. They should have been same.", oldEntity.GetDisplayName(), newEntity.GetDisplayName())
				}

				// finished comparing entityDTO, delete the entry from subMap.
				delete(entitiesFromNew, id)
			}
			if len(entitiesFromNew) != 0 {
				msg := fmt.Sprintf("Check %s: new has %d more entities discovered: ", entityType, len(entitiesFromNew))
				for id, e := range entitiesFromNew {
					msg = fmt.Sprintf("%s%s with id %s ", msg, e.GetDisplayName(), id)
				}
				glog.Errorf("%s", msg)
			}
		} else {
			glog.Errorf("%s was discovered by old, but not by new", entityType)
		}
	}

	if len(newGroup) != 0 {
		msg := fmt.Sprintf("newGroup has %d more entity types: ", len(newGroup))
		for entityType := range oldGroup {
			msg = fmt.Sprintf("%s%s, ", msg, entityType)
		}
		glog.Errorf("%s", msg)
	}
}

func showEntityDTOListDifference(oldEntityDTOs, newEntityDTOs map[string]*proto.EntityDTO) {
	newBackup := make(map[string]*proto.EntityDTO)
	for id, entityDTO := range newEntityDTOs {
		newBackup[id] = entityDTO
	}
	for id, entityDTO := range oldEntityDTOs {
		if _, exist := newBackup[id]; exist {
			delete(newBackup, id)
		} else {
			glog.Errorf("%s %s is not discovered in the new discovery framework.", entityDTO.EntityType, entityDTO.GetDisplayName())
		}
	}

	for id, entityDTO := range newBackup {
		glog.Errorf("%s %s is not discovered in the old discovery framework.", entityDTO.EntityType, entityDTO.GetDisplayName())
		newBackup[id] = entityDTO
	}
}

func groupByEntityDtoType(entityDTOs []*proto.EntityDTO) map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO {
	grouped := make(map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO)
	for _, entityDTO := range entityDTOs {
		mm, exist := grouped[entityDTO.GetEntityType()]
		if !exist {
			mm = make(map[string]*proto.EntityDTO)
		}
		mm[entityDTO.GetId()] = entityDTO
		grouped[entityDTO.GetEntityType()] = mm
	}
	return grouped
}

func compareEntityDTOs(oldEntity, newEntity *proto.EntityDTO) bool {
	isClear := true

	// entityType
	if oldEntity.GetEntityType() != newEntity.GetEntityType() {
		glog.Errorf("Entity type check: Old had entity type as %s, but new has it as %s.",
			oldEntity.GetDisplayName(), newEntity.GetDisplayName())
		// TODO should we return?
		isClear = false
	}
	entityType := oldEntity.GetEntityType()

	// entityID
	if oldEntity.GetId() != newEntity.GetId() {
		glog.Errorf("%s check: Old had entity ID as %s, but new has it as %s.", entityType,
			oldEntity.GetDisplayName(), newEntity.GetDisplayName())
		// TODO should we return?
		isClear = false
	}

	// display name
	if oldEntity.GetDisplayName() != newEntity.GetDisplayName() {
		glog.Errorf("%s check: Old had display name as %s, but new has it as %s.", entityType,
			oldEntity.GetDisplayName(), newEntity.GetDisplayName())
		isClear = false
	}
	displayName := newEntity.GetDisplayName()

	// monitored
	if oldEntity.GetMonitored() != newEntity.GetMonitored() {
		glog.Errorf("%s - %s check: Old had monitored as %t, but new has it as %t.", entityType, displayName,
			oldEntity.GetMonitored(), newEntity.GetMonitored())
		isClear = false
	}

	// origin
	if !reflect.DeepEqual(oldEntity.GetOrigin(), newEntity.GetOrigin()) {
		glog.Errorf("%s - %s check: Old had origin as %s, but new has it as %s.", entityType, displayName,
			oldEntity.GetOrigin(), newEntity.GetOrigin())
		isClear = false
	}

	// power state
	if oldEntity.GetPowerState() != newEntity.GetPowerState() {
		glog.Errorf("%s - %s check: Old had power state as %s, but new has it as %s.", entityType, displayName,
			oldEntity.GetPowerState(), newEntity.GetPowerState())
		isClear = false
	}

	// consumer policy
	if !reflect.DeepEqual(oldEntity.GetConsumerPolicy(), newEntity.GetConsumerPolicy()) {
		glog.Errorf("%s - %s check: Old had consumer policy as %s, but new has it as %s.", entityType, displayName,
			oldEntity.GetConsumerPolicy(), newEntity.GetConsumerPolicy())
		isClear = false
	}

	// provider policy
	if !reflect.DeepEqual(oldEntity.GetProviderPolicy(), newEntity.GetProviderPolicy()) {
		glog.Errorf("%s - %s check: Old had provider policy as %s, but new has it as %s.", entityType, displayName,
			oldEntity.GetProviderPolicy(), newEntity.GetProviderPolicy())
		isClear = false
	}

	// profile ID
	if oldEntity.GetProfileId() != newEntity.GetProfileId() {
		glog.Errorf("%s - %s check: Old had profile ID as %s, but new has it as %s.", entityType, displayName,
			oldEntity.GetProfileId(), newEntity.GetProfileId())
		isClear = false
	}

	// power state
	if oldEntity.GetPowerState() != newEntity.GetPowerState() {
		glog.Errorf("%s - %s check: Old had power state as %s, but new has it as %s.", entityType, displayName,
			oldEntity.GetPowerState(), newEntity.GetPowerState())
		isClear = false
	}

	// keep standalone
	if oldEntity.GetKeepStandalone() != newEntity.GetKeepStandalone() {
		glog.Errorf("%s - %s check: Old had keep standalone as %t, but new has it as %t.", entityType, displayName,
			oldEntity.GetKeepStandalone(), newEntity.GetKeepStandalone())
		isClear = false
	}

	// replacement data
	if !reflect.DeepEqual(oldEntity.GetReplacementEntityData(), newEntity.GetReplacementEntityData()) {
		glog.Errorf("%s - %s check: Old had replacement entity data as %s, but new has it as %s.", entityType, displayName,
			oldEntity.GetReplacementEntityData(), newEntity.GetReplacementEntityData())
		isClear = false
	}

	// entity data
	if !reflect.DeepEqual(oldEntity.GetEntityData(), newEntity.GetEntityData()) {
		glog.Errorf("%s - %s check: Old had entity data as %++v, but new has it as %++v.", entityType, displayName,
			oldEntity.GetEntityData(), newEntity.GetEntityData())
		isClear = false
	}

	// related entity data
	if !reflect.DeepEqual(oldEntity.GetRelatedEntityData(), newEntity.GetRelatedEntityData()) {
		glog.Errorf("%s - %s check: Old had related entity data as %++v, but new has it as %++v.", entityType, displayName,
			oldEntity.GetRelatedEntityData(), newEntity.GetRelatedEntityData())
		isClear = false
	}

	// related entity properties
	if !compareEntityProperties(oldEntity.GetEntityProperties(), newEntity.GetEntityProperties()) {
		glog.Errorf("%s - %s check: Old had properties as %++v, but new has it as %++v.", entityType, displayName,
			oldEntity.GetEntityProperties(), newEntity.GetEntityProperties())
		isClear = false
	}

	// commodity sold
	if !compareCommodities(oldEntity.GetCommoditiesSold(), newEntity.GetCommoditiesSold()) {
		glog.Errorf("%s - %s check: commodities sold check failed", entityType, displayName)
		isClear = false
	}

	// commodity bought
	oldCommoditiesBoughtGroup := groupCommoditiesBought(oldEntity.GetCommoditiesBought())
	newCommoditiesBoughtGroup := groupCommoditiesBought(newEntity.GetCommoditiesBought())
	for provider, commoditiesBoughtOld := range oldCommoditiesBoughtGroup {
		commoditiesBoughtNew, find := newCommoditiesBoughtGroup[provider]
		if !find {
			glog.Errorf("old had commodities bought from %s, but didn't find it in new.", provider)
			isClear = false
		} else {
			isClear = compareCommodities(commoditiesBoughtOld, commoditiesBoughtNew)
			if !isClear {
				glog.Errorf("%s - %s check: commodity bought from %s check failed", entityType, displayName, provider)
			}
		}
		// finished comparing current commodity bought provider. Delete the entry from map.
		delete(newCommoditiesBoughtGroup, provider)
	}
	if len(newCommoditiesBoughtGroup) != 0 {
		msg := fmt.Sprintf("New commodities bought group has %d provider: ", len(newCommoditiesBoughtGroup))
		for provider := range newCommoditiesBoughtGroup {
			msg = fmt.Sprintf("%s%s, ", msg, provider)
		}
		glog.Errorf("%s", msg)
		isClear = false
	}

	return isClear
}

func compareEntityProperties(properties1, properties2 []*proto.EntityDTO_EntityProperty) bool {
	if len(properties1) != len(properties2) {
		return false
	}
	p2Checked := 0
	for _, p1 := range properties1 {
		found := false
		for _, p2 := range properties2 {
			if reflect.DeepEqual(p1, p2) {
				found = true
				break
			}
		}
		if found {
			p2Checked++
		} else {
			return false
		}
	}
	if p2Checked != len(properties2) {
		return false
	}
	return true
}

func groupCommoditiesBought(commoditiesBought []*proto.EntityDTO_CommodityBought) map[string][]*proto.CommodityDTO {
	providerCommBoughtMap := make(map[string][]*proto.CommodityDTO)
	for _, commodityBought := range commoditiesBought {
		providerID := commodityBought.GetProviderId()
		commodities, exists := providerCommBoughtMap[providerID]
		if !exists {
			commodities = []*proto.CommodityDTO{}
		}
		commodities = append(commodities, commodityBought.GetBought()...)
		providerCommBoughtMap[providerID] = commodities
	}
	return providerCommBoughtMap
}

// <commodity_type : <commodity_key : commodityDTO>>
func groupCommodities(commodities []*proto.CommodityDTO) map[proto.CommodityDTO_CommodityType]map[string]*proto.CommodityDTO {
	grouped := make(map[proto.CommodityDTO_CommodityType]map[string]*proto.CommodityDTO)
	for _, comm := range commodities {
		cType := comm.GetCommodityType()
		subMap, exist := grouped[cType]
		if !exist {
			subMap = make(map[string]*proto.CommodityDTO)
		}
		key := comm.GetKey()
		subMap[key] = comm
		grouped[cType] = subMap
	}

	return grouped
}

func compareCommodities(commoditiesFromOld, commoditiesFromNew []*proto.CommodityDTO) bool {
	oldGroup := groupCommodities(commoditiesFromOld)
	newGroup := groupCommodities(commoditiesFromNew)

	isClear := true
	for cType, subOld := range oldGroup {
		subNew, exist := newGroup[cType]
		if !exist {
			glog.Errorf("Old had %s, but cannot find in new.", cType)
			isClear = false
			continue
		}

		for key, commOld := range subOld {
			commNew, find := subNew[key]
			if !find {
				glog.Errorf("Old had a %s commodity with key %s, but cannot find in new.", cType, key)
				isClear = false
				continue
			}

			if !compareCommodity(commOld, commNew) {
				isClear = false
			}

			delete(subNew, key)
		}

		if len(subNew) != 0 {
			msg := fmt.Sprintf("new has %d mismatched %s commodities: ", len(subNew), cType)
			for _, comm := range subNew {
				msg = fmt.Sprintf("%s%++v, ", msg, comm)
			}
			glog.Errorf("%s", msg)
			isClear = false
		}

		// finished comparing, delete the entry from map.
		delete(newGroup, cType)
	}

	if len(newGroup) != 0 {
		msg := fmt.Sprintf("new has %d more commodity types: ", len(newGroup))
		for cType := range newGroup {
			msg = fmt.Sprintf("%s%s, ", msg, cType)
		}
		glog.Errorf("%s", msg)
		isClear = false
	}

	return isClear
}

func compareCommodity(commodityOld, commodityNew *proto.CommodityDTO) bool {
	isClear := true

	// commodity type
	if commodityOld.GetCommodityType() != commodityNew.GetCommodityType() {
		glog.Errorf("Old has type as %s, new has it as %s", commodityOld.GetCommodityType(), commodityNew.GetCommodityType())
		isClear = false
	}
	cType := commodityOld.GetCommodityType()

	// key
	if commodityOld.GetKey() != commodityNew.GetKey() {
		glog.Errorf("Old has a %s commodity with key %s, new has it as %s", cType, commodityOld.GetKey(), commodityNew.GetKey())
		isClear = false
	}

	// active
	if commodityOld.GetActive() != commodityNew.GetActive() {
		glog.Errorf("Old has a %s commodity with active as %t, new has it as %t", cType, commodityOld.GetActive(), commodityNew.GetResizable())
		isClear = false
	}

	// resizable
	if commodityOld.GetResizable() != commodityNew.GetResizable() {
		glog.Errorf("Old has a %s commodity with resizable as %t, new has it as %t", cType, commodityOld.GetResizable(), commodityNew.GetResizable())
		isClear = false
	}

	// used value
	if !compareValue(commodityOld.GetUsed(), commodityNew.GetUsed()) {
		glog.Errorf("Old has a %s commodity used value set as %f, new has it as %f", cType, commodityOld.GetUsed(), commodityNew.GetUsed())
		isClear = false
	}

	// capacity value
	if !compareValue(commodityOld.GetCapacity(), commodityNew.GetCapacity()) {
		glog.Errorf("Old has a %s commodity capacity value set as %f, new has it as %f", cType, commodityOld.GetCapacity(), commodityNew.GetCapacity())
		isClear = false
	}

	// reservation value
	if !compareValue(commodityOld.GetReservation(), commodityNew.GetReservation()) {
		glog.Errorf("Old has a %s commodity reservation value set as %f, new has it as %f", cType, commodityOld.GetReservation(), commodityNew.GetReservation())
		isClear = false
	}
	return isClear
}

const toleration float64 = 0.3

// if the difference between two value is greater than toleration rate, then return false.
func compareValue(v1, v2 float64) bool {
	if v1 == 0 {
		if v2 == 0 {
			return true
		} else {
			return false
		}
	}
	return math.Abs(v1-v2)/math.Abs(v1) <= toleration
}
