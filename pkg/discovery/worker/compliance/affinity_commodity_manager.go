package compliance

import (
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/labels"
	api "k8s.io/client-go/pkg/api/v1"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"github.com/mitchellh/hashstructure"
)

type AffinityCommodityManager struct {
	commodityStore map[string]*proto.CommodityDTO
}

func NewAffinityCommodityManager() *AffinityCommodityManager {
	return &AffinityCommodityManager{
		commodityStore: make(map[string]*proto.CommodityDTO),
	}
}

func (acm *AffinityCommodityManager) GetAccessCommoditiesForNodeAffinity(matchExpressions []api.NodeSelectorRequirement) ([]*proto.CommodityDTO, error) {
	var accessComms []*proto.CommodityDTO
	nodeSelector, err := NodeSelectorRequirementsAsSelector(matchExpressions)
	if err != nil {
		glog.V(4).Infof("Failed to parse MatchExpressions: %+v, regarding as not match.", matchExpressions)
		return nil, fmt.Errorf("failed to build selector based on given expressions: %s", err)
	}
	comm, err := acm.getCommodity(nodeSelector)
	if err != nil {
		// return immediately even if only one failed.
		return nil, fmt.Errorf("failed ot get accessCommodityDTO based on given expressions: %s", err)
	}
	accessComms = append(accessComms, comm)
	glog.Infof("Access Comm is build %+v", accessComms)
	return accessComms, nil
}

func (acm *AffinityCommodityManager) getCommodity(selector labels.Selector) (*proto.CommodityDTO, error) {

	hashCode, err := hashstructure.Hash(selector, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to generate hash: %s", err)
	}

	key := strconv.FormatUint(hashCode, 10)
	commodityDTO, exist := acm.commodityStore[key]
	if exist {
		return commodityDTO, nil
	}
	commodityDTO, err = sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
		Key(key).
		Create()
	if err != nil {
		return nil, err
	}

	// put into store.
	acm.commodityStore[key] = commodityDTO

	return commodityDTO, nil
}

