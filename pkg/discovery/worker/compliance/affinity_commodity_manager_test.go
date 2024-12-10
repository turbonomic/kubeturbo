package compliance

import (
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"reflect"
	"strconv"
	"testing"

	"github.com/mitchellh/hashstructure"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

func TestGetCommoditySold(t *testing.T) {
	accessCommodityType := proto.CommodityDTO_VMPM_ACCESS
	accessCommodityDefaultCapacity := accessCommodityDefaultCapacity

	table := []struct {
		key                  string
		commSoldStore        map[string]*proto.CommodityDTO
		expectedCommodityDTO *proto.CommodityDTO
	}{
		{
			key: "nodeSelectorTermStringHash",
			expectedCommodityDTO: &proto.CommodityDTO{
				CommodityType: &accessCommodityType,
				Key:           func() *string { s := "nodeSelectorTermStringHash"; return &s }(),
				Capacity:      &accessCommodityDefaultCapacity,
			},
		},
		{
			key: "nodeSelectorTermStringHash_1",
			commSoldStore: map[string]*proto.CommodityDTO{
				"nodeSelectorTermStringHash_1": {
					CommodityType: &accessCommodityType,
					Key:           func() *string { s := "nodeSelectorTermStringHash_1"; return &s }(),
					Capacity:      &accessCommodityDefaultCapacity,
				},
			},
			expectedCommodityDTO: &proto.CommodityDTO{
				CommodityType: &accessCommodityType,
				Key:           func() *string { s := "nodeSelectorTermStringHash_1"; return &s }(),
				Capacity:      &accessCommodityDefaultCapacity,
			},
		},
		{
			key: "nodeSelectorTermStringHash_2",
			commSoldStore: map[string]*proto.CommodityDTO{
				"nodeSelectorTermStringHash_1": {
					CommodityType: &accessCommodityType,
					Key:           func() *string { s := "nodeSelectorTermStringHash_1"; return &s }(),
					Capacity:      &accessCommodityDefaultCapacity,
				},
			},
			expectedCommodityDTO: &proto.CommodityDTO{
				CommodityType: &accessCommodityType,
				Key:           func() *string { s := "nodeSelectorTermStringHash_2"; return &s }(),
				Capacity:      &accessCommodityDefaultCapacity,
			},
		},
	}

	for i, item := range table {
		acm := NewAffinityCommodityManager()
		if item.commSoldStore != nil {
			acm.commoditySoldStore = item.commSoldStore
		}

		comm, err := acm.getCommoditySold(item.key)
		if err != nil {
			t.Errorf("Test case %d failed: unexpected error: %s", i, err)
		}
		if !reflect.DeepEqual(item.expectedCommodityDTO, comm) {
			t.Errorf("Test case %d failed: expects %++v, got %++v", i, item.expectedCommodityDTO, comm)
		}
	}
}

func TestGetCommodityBought(t *testing.T) {
	accessCommodityType := proto.CommodityDTO_VMPM_ACCESS

	table := []struct {
		key                  string
		commBoughtStore      map[string]*proto.CommodityDTO
		expectedCommodityDTO *proto.CommodityDTO
	}{
		{
			key: "nodeSelectorTermStringHash",
			expectedCommodityDTO: &proto.CommodityDTO{
				CommodityType: &accessCommodityType,
				Key:           func() *string { s := "nodeSelectorTermStringHash"; return &s }(),
			},
		},
		{
			key: "nodeSelectorTermStringHash_1",
			commBoughtStore: map[string]*proto.CommodityDTO{
				"nodeSelectorTermStringHash_1": {
					CommodityType: &accessCommodityType,
					Key:           func() *string { s := "nodeSelectorTermStringHash_1"; return &s }(),
				},
			},
			expectedCommodityDTO: &proto.CommodityDTO{
				CommodityType: &accessCommodityType,
				Key:           func() *string { s := "nodeSelectorTermStringHash_1"; return &s }(),
			},
		},
		{
			key: "nodeSelectorTermStringHash_2",
			commBoughtStore: map[string]*proto.CommodityDTO{
				"nodeSelectorTermStringHash_1": {
					CommodityType: &accessCommodityType,
					Key:           func() *string { s := "nodeSelectorTermStringHash_1"; return &s }(),
				},
			},
			expectedCommodityDTO: &proto.CommodityDTO{
				CommodityType: &accessCommodityType,
				Key:           func() *string { s := "nodeSelectorTermStringHash_2"; return &s }(),
			},
		},
	}

	for i, item := range table {
		acm := NewAffinityCommodityManager()
		if item.commBoughtStore != nil {
			acm.commodityBoughtStore = item.commBoughtStore
		}

		comm, err := acm.getCommodityBought(item.key)
		if err != nil {
			t.Errorf("Test %d failed: unexpected error: %s", i, err)
		}
		if !reflect.DeepEqual(item.expectedCommodityDTO, comm) {
			t.Errorf("Test case %d failed: expects %++v, got %++v", i, item.expectedCommodityDTO, comm)
		}
	}
}

func TestGetCommoditySoldAndBought(t *testing.T) {
	table := []struct {
		termString                 string
		expectedCommodityDTOBought *proto.CommodityDTO
		expectedCommodityDTOSold   *proto.CommodityDTO
	}{
		{
			termString: "nodeSelectorTermStringHash",
			expectedCommodityDTOBought: &proto.CommodityDTO{
				CommodityType: func() *proto.CommodityDTO_CommodityType {
					t := proto.CommodityDTO_VMPM_ACCESS
					return &t
				}(),
				Key: func() *string {
					s := getKey("nodeSelectorTermStringHash")
					s = "nodeSelectorTermStringHash" + "|" + s
					return &s
				}(),
			},
			expectedCommodityDTOSold: &proto.CommodityDTO{
				CommodityType: func() *proto.CommodityDTO_CommodityType {
					t := proto.CommodityDTO_VMPM_ACCESS
					return &t
				}(),
				Capacity: func() *float64 {
					c := accessCommodityDefaultCapacity
					return &c
				}(),
				Key: func() *string {
					s := getKey("nodeSelectorTermStringHash")
					s = "nodeSelectorTermStringHash" + "|" + s
					return &s
				}(),
			},
		},
	}

	for i, item := range table {
		acm := NewAffinityCommodityManager()
		commSold, commBought, err := acm.getCommoditySoldAndBought(item.termString, item.termString)
		if err != nil {
			t.Errorf("Test case %d failed: unexpected error: %s", i, err)
		}
		if !reflect.DeepEqual(item.expectedCommodityDTOSold, commSold) {
			t.Errorf("Test case %d failed: commoditySold: expects %++v, got %++v", i, item.expectedCommodityDTOSold, commSold)
		}
		if !reflect.DeepEqual(item.expectedCommodityDTOBought, commBought) {
			t.Errorf("Test case %d failed: commodityBought: expects %++v, got %++v", i, item.expectedCommodityDTOBought, commBought)
		}
	}
}

func TestGetAccessCommoditiesForPodAffinityAntiAffinity(t *testing.T) {
	table := []struct {
		terms []api.PodAffinityTerm
	}{
		{
			terms: []api.PodAffinityTerm{
				{
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "service",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"securityscan", "value1"},
							},
						},
					},
					TopologyKey: "region",
				},
				{
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "zone",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"east1", "east2"},
							},
						},
					},
					Namespaces:  []string{"default", "ns1"},
					TopologyKey: "hostname",
				},
			},
		},
	}

	for i, item := range table {
		cType := proto.CommodityDTO_VMPM_ACCESS
		capacity := accessCommodityDefaultCapacity

		expectedCommoditiesSold := []*proto.CommodityDTO{}
		expectedCommoditiesBought := []*proto.CommodityDTO{}
		for _, term := range item.terms {
			// The '/' replaces an empty 'namespace/podname' string in this test
			key := getReadablePodAffinityTermString(term) + "|/|" + getKey(term.String())
			expectedCommoditiesSold = append(expectedCommoditiesSold, &proto.CommodityDTO{

				CommodityType: &cType,
				Capacity:      &capacity,
				Key:           &key,
			})
			expectedCommoditiesBought = append(expectedCommoditiesBought, &proto.CommodityDTO{
				CommodityType: &cType,
				Key:           &key,
			})
		}
		acm := NewAffinityCommodityManager()

		p := &api.Pod{}
		commsSold, commsBought, err := acm.GetAccessCommoditiesForPodAffinityAntiAffinity(item.terms, p)
		if err != nil {
			t.Errorf("Test case %d failed: unexpected error: %s", i, err)
		}
		if !reflect.DeepEqual(expectedCommoditiesSold, commsSold) {
			t.Errorf("Test case %d failed: commoditySold: expects %++v, got %++v", i, expectedCommoditiesSold, commsSold)
		}
		if !reflect.DeepEqual(expectedCommoditiesBought, commsBought) {
			t.Errorf("Test case %d failed: commodityBought: expects %++v, got %++v", i, expectedCommoditiesBought, commsBought)
		}
	}
}

func TestGetAccessCommoditiesForNodeAffinity(t *testing.T) {
	table := []struct {
		terms []api.NodeSelectorTerm
	}{
		{
			terms: []api.NodeSelectorTerm{
				{
					MatchExpressions: []api.NodeSelectorRequirement{
						{
							Key:      "foo",
							Operator: api.NodeSelectorOpIn,
							Values:   []string{"bar", "value2"},
						},
					},
				},

				{
					MatchExpressions: []api.NodeSelectorRequirement{
						{
							Key:      "zone",
							Operator: api.NodeSelectorOpIn,
							Values:   []string{"east1", "east2"},
						},
					},
				},
			},
		},
	}

	for i, item := range table {
		cType := proto.CommodityDTO_VMPM_ACCESS
		capacity := accessCommodityDefaultCapacity

		expectedCommoditiesSold := []*proto.CommodityDTO{}
		expectedCommoditiesBought := []*proto.CommodityDTO{}
		for _, term := range item.terms {
			key := getReadableNodeSelectorTermString(term) + "|" + getKey(term.String())
			expectedCommoditiesSold = append(expectedCommoditiesSold, &proto.CommodityDTO{

				CommodityType: &cType,
				Capacity:      &capacity,
				Key:           &key,
			})
			expectedCommoditiesBought = append(expectedCommoditiesBought, &proto.CommodityDTO{
				CommodityType: &cType,
				Key:           &key,
			})
		}
		acm := NewAffinityCommodityManager()

		commsSold, commsBought, err := acm.GetAccessCommoditiesForNodeAffinity(item.terms)
		if err != nil {
			t.Errorf("Test case %d failed: unexpected error: %s", i, err)
		}
		if !reflect.DeepEqual(expectedCommoditiesSold, commsSold) {
			t.Errorf("Test case %d failed: commoditySold: expects %++v, got %++v", i, expectedCommoditiesSold, commsSold)
		}
		if !reflect.DeepEqual(expectedCommoditiesBought, commsBought) {
			t.Errorf("Test case %d failed: commodityBought: expects %++v, got %++v", i, expectedCommoditiesBought, commsBought)
		}
	}
}

func getKey(termString string) string {
	hashCode, err := hashstructure.Hash(termString, nil)
	if err != nil {
		return ""
	}

	key := strconv.FormatUint(hashCode, 10)
	return key
}
