package compliance

import (
	"reflect"
	"testing"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

func TestGetEntityDTO(t *testing.T) {
	table := []struct {
		entityMaps map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO
		eTypeInput proto.EntityDTO_EntityType
		eIDInput   string

		expectsErr  bool
		expectedDTO *proto.EntityDTO
	}{
		{
			entityMaps: map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO{
				proto.EntityDTO_VIRTUAL_MACHINE: map[string]*proto.EntityDTO{
					"foo": {
						EntityType: getEntityTypePointer(proto.EntityDTO_VIRTUAL_MACHINE),
						Id:         getStringPointer("foo"),
					},
				},
				proto.EntityDTO_CONTAINER_POD: map[string]*proto.EntityDTO{
					"bar": {
						EntityType: getEntityTypePointer(proto.EntityDTO_CONTAINER_POD),
						Id:         getStringPointer("bar"),
					},
				},
			},
			eTypeInput: proto.EntityDTO_VIRTUAL_MACHINE,
			eIDInput:   "foo",

			expectsErr: false,
			expectedDTO: &proto.EntityDTO{
				EntityType: getEntityTypePointer(proto.EntityDTO_VIRTUAL_MACHINE),
				Id:         getStringPointer("foo"),
			},
		},
		{
			entityMaps: map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO{
				proto.EntityDTO_VIRTUAL_MACHINE: map[string]*proto.EntityDTO{
					"foo": {
						EntityType: getEntityTypePointer(proto.EntityDTO_VIRTUAL_MACHINE),
						Id:         getStringPointer("foo"),
					},
				},
			},
			eTypeInput: proto.EntityDTO_VIRTUAL_MACHINE,
			eIDInput:   "bar",

			expectsErr: true,
		},
		{
			eTypeInput: proto.EntityDTO_VIRTUAL_MACHINE,
			eIDInput:   "foo",
			expectsErr: true,
		},
	}
	for i, item := range table {
		cProcessor := NewComplianceProcessor()
		if item.entityMaps != nil {
			cProcessor.entityMaps = item.entityMaps
		}
		entityDTO, err := cProcessor.GetEntityDTO(item.eTypeInput, item.eIDInput)
		if err != nil && !item.expectsErr {
			t.Errorf("Test case %d failed: ecpected error: %s", i, err)
		}
		if err == nil && item.expectsErr {
			t.Errorf("Test case %d failed: didn't get any expected error.", i)
		}
		if !reflect.DeepEqual(item.expectedDTO, entityDTO) {
			t.Errorf("Test case %d failed: expect %++v, got %++v", i, item.expectedDTO, entityDTO)
		}
	}
}

func TestUpdateEntityDTO(t *testing.T) {
	table := []struct {
		entityMaps map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO
		updatedDTO *proto.EntityDTO

		expectsErr bool
	}{
		{
			entityMaps: map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO{
				proto.EntityDTO_VIRTUAL_MACHINE: map[string]*proto.EntityDTO{
					"foo": {
						EntityType:  getEntityTypePointer(proto.EntityDTO_VIRTUAL_MACHINE),
						Id:          getStringPointer("foo"),
						DisplayName: getStringPointer("original"),
					},
				},
			},
			updatedDTO: &proto.EntityDTO{
				EntityType:  getEntityTypePointer(proto.EntityDTO_VIRTUAL_MACHINE),
				Id:          getStringPointer("foo"),
				DisplayName: getStringPointer("newDisplayName"),
			},

			expectsErr: false,
		},
		{
			entityMaps: map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO{
				proto.EntityDTO_VIRTUAL_MACHINE: map[string]*proto.EntityDTO{
					"foo": {
						EntityType:  getEntityTypePointer(proto.EntityDTO_VIRTUAL_MACHINE),
						Id:          getStringPointer("foo"),
						DisplayName: getStringPointer("original"),
					},
				},
			},
			updatedDTO: &proto.EntityDTO{
				EntityType:  getEntityTypePointer(proto.EntityDTO_CONTAINER_POD),
				Id:          getStringPointer("foo"),
				DisplayName: getStringPointer("newDisplayName"),
			},

			expectsErr: true,
		},
		{
			entityMaps: map[proto.EntityDTO_EntityType]map[string]*proto.EntityDTO{
				proto.EntityDTO_VIRTUAL_MACHINE: map[string]*proto.EntityDTO{
					"foo": {
						EntityType:  getEntityTypePointer(proto.EntityDTO_VIRTUAL_MACHINE),
						Id:          getStringPointer("foo"),
						DisplayName: getStringPointer("original"),
					},
				},
			},
			updatedDTO: &proto.EntityDTO{
				EntityType:  getEntityTypePointer(proto.EntityDTO_VIRTUAL_MACHINE),
				Id:          getStringPointer("bar"),
				DisplayName: getStringPointer("newDisplayName"),
			},

			expectsErr: true,
		},
	}

	for i, item := range table {
		cProcessor := NewComplianceProcessor()
		if item.entityMaps != nil {
			cProcessor.entityMaps = item.entityMaps
		}
		err := cProcessor.UpdateEntityDTO(item.updatedDTO)
		if err != nil && !item.expectsErr {
			t.Errorf("Test case %d failed: ecpected error: %s", i, err)
		}
		if err == nil {
			if item.expectsErr {
				t.Errorf("Test case %d failed: didn't get any expected error.", i)
			} else {
				entityDTO, err := cProcessor.GetEntityDTO(item.updatedDTO.GetEntityType(), item.updatedDTO.GetId())
				if err != nil {
					t.Errorf("Test case %d failed: cannot find entityDTO after updating, err is %s", i, err)
				}
				if !reflect.DeepEqual(item.updatedDTO, entityDTO) {
					t.Errorf("Updated entityDTO is %++v, got %++v", item.updatedDTO, entityDTO)
				}
			}
		}
	}
}

func TestAddCommoditiesSold(t *testing.T) {
	table := []struct {
		entityDTO   *proto.EntityDTO
		commodities []*proto.CommodityDTO

		expectsErr bool
	}{
		{
			entityDTO: &proto.EntityDTO{
				EntityType: getEntityTypePointer(proto.EntityDTO_VIRTUAL_MACHINE),
				Id:         getStringPointer("foo"),
			},
			commodities: []*proto.CommodityDTO{
				{
					CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
					Key:           getStringPointer("some_key"),
				},
			},

			expectsErr: false,
		},
		{
			entityDTO: &proto.EntityDTO{
				EntityType: getEntityTypePointer(proto.EntityDTO_VIRTUAL_MACHINE),
				Id:         getStringPointer("foo"),
				CommoditiesSold: []*proto.CommodityDTO{
					{
						CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
						Key:           getStringPointer("some_key"),
					},
				},
			},
			commodities: []*proto.CommodityDTO{
				{
					CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
					Key:           getStringPointer("some_key"),
				},
			},

			expectsErr: false,
		},
		{
			commodities: []*proto.CommodityDTO{
				{
					CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
					Key:           getStringPointer("some_key"),
				},
			},

			expectsErr: true,
		},
		{
			entityDTO: &proto.EntityDTO{
				EntityType: getEntityTypePointer(proto.EntityDTO_VIRTUAL_MACHINE),
				Id:         getStringPointer("foo"),
				CommoditiesSold: []*proto.CommodityDTO{
					{
						CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
						Key:           getStringPointer("some_key"),
					},
				},
			},
			expectsErr: true,
		},
	}

	for i, item := range table {
		cProcessor := NewComplianceProcessor()
		cProcessor.entityMaps[item.entityDTO.GetEntityType()] = make(map[string]*proto.EntityDTO)
		cProcessor.entityMaps[item.entityDTO.GetEntityType()][item.entityDTO.GetId()] = item.entityDTO

		err := cProcessor.AddCommoditiesSold(item.entityDTO, item.commodities...)
		if item.expectsErr {
			if err == nil {
				t.Errorf("Test case %d failed. Didn't get the expected error.", i)
			}
		} else {
			if err != nil {
				t.Errorf("Test case %d failed. Unexpected error: %s", i, err)
			} else {
				entityDTO, err := cProcessor.GetEntityDTO(item.entityDTO.GetEntityType(), item.entityDTO.GetId())
				if err != nil {
					t.Errorf("Test case %d failed. Unexpected error when try to get the target entityDTO: %s", i, err)
				} else {
					for _, commSold := range item.commodities {
						if !hasCommoditySold(entityDTO, commSold) {
							t.Errorf("Test case %d failed: didn't find the expected commodity: %++v", i, commSold)
						}
					}
				}
			}
		}

	}
}

func TestAddCommodityBought(t *testing.T) {
	table := []struct {
		entityDTO   *proto.EntityDTO
		commodities []*proto.CommodityDTO
		provider    *sdkbuilder.ProviderDTO

		expectsErr bool
	}{
		{
			// new provider
			entityDTO: &proto.EntityDTO{
				EntityType: getEntityTypePointer(proto.EntityDTO_CONTAINER_POD),
				Id:         getStringPointer("foo"),
			},
			commodities: []*proto.CommodityDTO{
				{
					CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
					Key:           getStringPointer("some_key"),
				},
			},
			provider: sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, "vm_key_foo"),

			expectsErr: false,
		},
		{
			// same commodity and different commodity
			entityDTO: &proto.EntityDTO{
				EntityType: getEntityTypePointer(proto.EntityDTO_CONTAINER_POD),
				Id:         getStringPointer("foo"),
				CommoditiesBought: []*proto.EntityDTO_CommodityBought{
					{
						ProviderId: getStringPointer("vm_key_foo"),
						Bought: []*proto.CommodityDTO{
							{
								CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
								Key:           getStringPointer("commodity_key_foo"),
							},
						},
					},
				},
			},
			commodities: []*proto.CommodityDTO{
				{
					CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
					Key:           getStringPointer("commodity_key_foo"),
				},
				{
					CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
					Key:           getStringPointer("commodity_key_bar"),
				},
			},
			provider: sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, "vm_key_foo"),

			expectsErr: false,
		},
		{
			// different provider key
			entityDTO: &proto.EntityDTO{
				EntityType: getEntityTypePointer(proto.EntityDTO_CONTAINER_POD),
				Id:         getStringPointer("foo"),
				CommoditiesBought: []*proto.EntityDTO_CommodityBought{
					{
						ProviderId: getStringPointer("vm_key_foo"),
						Bought: []*proto.CommodityDTO{
							{
								CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
								Key:           getStringPointer("commodity_key_foo"),
							},
						},
					},
				},
			},
			commodities: []*proto.CommodityDTO{
				{
					CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
					Key:           getStringPointer("commodity_key_foo"),
				},
			},
			provider: sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, "vm_key_bar"),

			expectsErr: false,
		},
		{
			// miss provider
			entityDTO: &proto.EntityDTO{
				EntityType: getEntityTypePointer(proto.EntityDTO_VIRTUAL_MACHINE),
				Id:         getStringPointer("foo"),
				CommoditiesSold: []*proto.CommodityDTO{
					{
						CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
						Key:           getStringPointer("some_key"),
					},
				},
			},
			commodities: []*proto.CommodityDTO{
				{
					CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
					Key:           getStringPointer("some_key"),
				},
			},

			expectsErr: true,
		},
		{
			// miss entityDTO
			commodities: []*proto.CommodityDTO{
				{
					CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
					Key:           getStringPointer("some_key"),
				},
			},
			provider: sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, "vm_key_bar"),

			expectsErr: true,
		},
		{
			// miss commodities.
			entityDTO: &proto.EntityDTO{
				EntityType: getEntityTypePointer(proto.EntityDTO_VIRTUAL_MACHINE),
				Id:         getStringPointer("foo"),
				CommoditiesSold: []*proto.CommodityDTO{
					{
						CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
						Key:           getStringPointer("some_key"),
					},
				},
			},
			provider:   sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, "vm_key_bar"),
			expectsErr: true,
		},
	}

	for i, item := range table {
		cProcessor := NewComplianceProcessor()
		cProcessor.entityMaps[item.entityDTO.GetEntityType()] = make(map[string]*proto.EntityDTO)
		cProcessor.entityMaps[item.entityDTO.GetEntityType()][item.entityDTO.GetId()] = item.entityDTO

		err := cProcessor.AddCommoditiesBought(item.entityDTO, item.provider, item.commodities...)
		if item.expectsErr {
			if err == nil {
				t.Errorf("Test case %d failed. Didn't get the expected error.", i)
			}
		} else {
			if err != nil {
				t.Errorf("Test case %d failed. Unexpected error: %s", i, err)
			} else {
				entityDTO, err := cProcessor.GetEntityDTO(item.entityDTO.GetEntityType(), item.entityDTO.GetId())
				if err != nil {
					t.Errorf("Test case %d failed. Unexpected error when try to get the target entityDTO: %s", i, err)
				} else {
					found := false
					for _, commBought := range entityDTO.GetCommoditiesBought() {
						if commBought.GetProviderId() == item.provider.GetId() {
							for _, comm := range item.commodities {
								if !hasCommodityBought(commBought, comm) {
									t.Errorf("Test case %d failed: didn't find the expected commodity: %++v", i, comm)
								}
							}
							found = true
						}
					}
					if !found {
						t.Errorf("Test case %d failed: didn't find the expected provider %v", i, item.provider)
					}
				}
			}
		}
	}
}

func TestHasCommodity(t *testing.T) {
	table := []struct {
		existingCommodities []*proto.CommodityDTO
		commodity           *proto.CommodityDTO

		expects bool
	}{
		{
			existingCommodities: []*proto.CommodityDTO{
				{
					CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
					Key:           getStringPointer("foo"),
				},
			},
			commodity: &proto.CommodityDTO{
				CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
				Key:           getStringPointer("foo"),
			},
			expects: true,
		},
		{
			existingCommodities: []*proto.CommodityDTO{
				{
					CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
					Key:           getStringPointer("foo"),
				},
			},
			commodity: &proto.CommodityDTO{
				CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
				Key:           getStringPointer("bar"),
			},
			expects: false,
		},
		{
			existingCommodities: []*proto.CommodityDTO{
				{
					CommodityType: getCommodityTypePointer(proto.CommodityDTO_CPU),
					Key:           getStringPointer("foo"),
				},
			},
			commodity: &proto.CommodityDTO{
				CommodityType: getCommodityTypePointer(proto.CommodityDTO_MEM),
				Key:           getStringPointer("foo"),
			},
			expects: false,
		},
	}

	for i, item := range table {
		found := hasCommodity(item.existingCommodities, item.commodity)
		if found != item.expects {
			t.Errorf("Test case %d failed. Expects %t, got %t", i, item.expects, found)
		}
	}
}

func getEntityTypePointer(t proto.EntityDTO_EntityType) *proto.EntityDTO_EntityType {
	return &t
}

func getCommodityTypePointer(t proto.CommodityDTO_CommodityType) *proto.CommodityDTO_CommodityType {
	return &t
}

func getStringPointer(s string) *string {
	return &s
}

func getFloat64Pointer(f float64) *float64 {
	return &f
}
