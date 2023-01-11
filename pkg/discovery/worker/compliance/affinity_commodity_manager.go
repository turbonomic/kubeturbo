package compliance

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/mitchellh/hashstructure"
)

const (
	accessCommodityDefaultCapacity = 1e10
)

type AffinityCommodityManager struct {
	commoditySoldStore   map[string]*proto.CommodityDTO
	commodityBoughtStore map[string]*proto.CommodityDTO
	commoditySoldLock    sync.RWMutex
	commodityBoughtLock  sync.RWMutex
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
		commSold, commBought, err := acm.getCommoditySoldAndBought(getReadableNodeSelectorTermString(term), term.String())
		if err != nil {
			return nil, nil, err
		}
		accessCommsSold = append(accessCommsSold, commSold)
		accessCommsBought = append(accessCommsBought, commBought)
	}
	return accessCommsSold, accessCommsBought, nil
}

func (acm *AffinityCommodityManager) GetAccessCommoditiesForPodAffinityAntiAffinity(podAffinityTerms []api.PodAffinityTerm, pod *api.Pod) ([]*proto.CommodityDTO, []*proto.CommodityDTO, error) {
	var accessCommsSold []*proto.CommodityDTO
	var accessCommsBought []*proto.CommodityDTO
	for _, term := range podAffinityTerms {
		// Add the pod name and namespace in the string to generate hash
		// to disambiguate the same term on a different pod.
		displayString := getReadablePodAffinityTermString(term) + "|" + pod.Namespace + "/" + pod.Name
		commSold, commBought, err := acm.getCommoditySoldAndBought(displayString, term.String())
		if err != nil {
			return nil, nil, err
		}
		accessCommsSold = append(accessCommsSold, commSold)
		accessCommsBought = append(accessCommsBought, commBought)
	}
	return accessCommsSold, accessCommsBought, nil
}

func (acm *AffinityCommodityManager) getCommoditySoldAndBought(displayString, termString string) (*proto.CommodityDTO, *proto.CommodityDTO, error) {
	hash, err := generateKey(termString)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate hash: %s", err)
	}
	key := displayString + "|" + hash

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
	acm.commoditySoldLock.Lock()
	defer acm.commoditySoldLock.Unlock()
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
	acm.commodityBoughtLock.Lock()
	defer acm.commodityBoughtLock.Unlock()
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

func getReadableNodeSelectorTermString(term api.NodeSelectorTerm) string {
	expressionsString, fieldsString := "", ""
	expressionsSelectors, err := NodeSelectorRequirementsAsSelector(term.MatchExpressions)
	if err != nil {
		expressionsString = "<error>"
	} else {
		expressionsString = expressionsSelectors.String()
	}
	fieldsSelectors, err := NodeSelectorRequirementsAsSelector(term.MatchFields)
	if err != nil {
		fieldsString = "<error>"
	} else {
		fieldsString = fieldsSelectors.String()
	}

	return "[" + expressionsString + "][" + fieldsString + "]"
}

func getReadablePodAffinityTermString(term api.PodAffinityTerm) string {
	selectorString := metav1.FormatLabelSelector(term.LabelSelector)
	namespaces := strings.Join(term.Namespaces, ",")

	return "[" + selectorString + "][Namespace: " + namespaces + "][TopologyKey: " + term.TopologyKey + "]"
}
