package compliance

import (
	"fmt"
	"strconv"

	api "k8s.io/client-go/pkg/api/v1"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/mitchellh/hashstructure"
)

const (
	accessCommodityDefaultCapacity = 1E10
)

type AffinityCommodityManager struct {
	commoditySoldStore   map[string]*proto.CommodityDTO
	commodityBoughtStore map[string]*proto.CommodityDTO
}

func NewAffinityCommodityManager() *AffinityCommodityManager {
	return &AffinityCommodityManager{
		commoditySoldStore:   make(map[string]*proto.CommodityDTO),
		commodityBoughtStore: make(map[string]*proto.CommodityDTO),
	}
}

func (acm *AffinityCommodityManager) GetAccessCommoditiesForNodeAffinity(nodeSelectorTerms []api.NodeSelectorTerm) ([]*proto.CommodityDTO, []*proto.CommodityDTO, error) {
	var accessCommsSold []*proto.CommodityDTO
	var accessCommsBought []*proto.CommodityDTO
	for _, term := range nodeSelectorTerms {
		commSold, commBought, err := acm.getCommoditySoldAndBought(term.String())
		if err != nil {
			return nil, nil, err
		}
		accessCommsSold = append(accessCommsSold, commSold)
		accessCommsBought = append(accessCommsBought, commBought)
	}
	return accessCommsSold, accessCommsBought, nil
}

func (acm *AffinityCommodityManager) GetAccessCommoditiesForPodAffinityAntiAffinity(podAffinityTerm []api.PodAffinityTerm) ([]*proto.CommodityDTO, []*proto.CommodityDTO, error) {
	var accessCommsSold []*proto.CommodityDTO
	var accessCommsBought []*proto.CommodityDTO
	for _, term := range podAffinityTerm {
		commSold, commBought, err := acm.getCommoditySoldAndBought(term.String())
		if err != nil {
			return nil, nil, err
		}
		accessCommsSold = append(accessCommsSold, commSold)
		accessCommsBought = append(accessCommsBought, commBought)
	}
	return accessCommsSold, accessCommsBought, nil
}

func (acm *AffinityCommodityManager) getCommoditySoldAndBought(termString string) (*proto.CommodityDTO, *proto.CommodityDTO, error) {
	key, err := generateKey(termString)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate hash: %s", err)
	}
	commSold, err := acm.getCommoditySold(key)
	if err != nil {
		// return immediately even if only one failed.
		return nil, nil, fmt.Errorf("failed to get accessCommodityDTO sold based on given expressions: %s", err)
	}

	commBought, err := acm.getCommodityBought(key)
	if err != nil {
		// return immediately even if only one failed.
		return nil, nil, fmt.Errorf("failed to get accessCommodityDTO bought based on given expressions: %s", err)
	}

	return commSold, commBought, nil
}

func (acm *AffinityCommodityManager) getCommoditySold(key string) (*proto.CommodityDTO, error) {
	commodityDTO, exist := acm.commoditySoldStore[key]
	if exist {
		return commodityDTO, nil
	}
	commodityDTO, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
		Key(key).
		Capacity(accessCommodityDefaultCapacity).
		Create()
	if err != nil {
		return nil, err
	}

	// put into store.
	acm.commoditySoldStore[key] = commodityDTO

	return commodityDTO, nil
}

func (acm *AffinityCommodityManager) getCommodityBought(key string) (*proto.CommodityDTO, error) {
	commodityDTO, exist := acm.commodityBoughtStore[key]
	if exist {
		return commodityDTO, nil
	}
	commodityDTO, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
		Key(key).
		Create()
	if err != nil {
		return nil, err
	}

	// put into store.
	acm.commodityBoughtStore[key] = commodityDTO

	return commodityDTO, nil
}

func generateKey(termString string) (string, error) {
	hashCode, err := hashstructure.Hash(termString, nil)
	if err != nil {
		return "", err
	}

	key := strconv.FormatUint(hashCode, 10)
	return key, nil
}
