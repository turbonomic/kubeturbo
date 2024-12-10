package registration

import (
	"fmt"
	"testing"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

func testVM(priority int32, isBase bool) error {
	f := NewSupplyChainFactory(stitching.IP, priority, isBase)

	dtos, err := f.createSupplyChain()
	if err != nil {
		return fmt.Errorf("Failed to create supply chain: %v", err)
	}

	for _, dto := range dtos {
		if dto.GetTemplateClass() == proto.EntityDTO_VIRTUAL_MACHINE {
			p := dto.GetTemplatePriority()
			flag := (dto.GetTemplateType() == proto.TemplateDTO_BASE)

			if p != priority {
				return fmt.Errorf("Wrong template priority: %d Vs. %d", p, priority)
			}

			if flag != isBase {
				return fmt.Errorf("Wrong tempalte type: %v", dto.GetTemplateType())
			}

			//fmt.Printf("vm Template: %++v\n", dto)
			break
		}
	}

	return nil
}

func TestNewSupplyChainFactory_VM(t *testing.T) {
	priorities := []int32{-100, 100, 10, -10, 1, 0, -1, -2, 8}
	isBases := []bool{true, false}

	for _, isBase := range isBases {
		for _, p := range priorities {
			err := testVM(p, isBase)
			if err != nil {
				t.Errorf("Test VM failed: %v", err)
			}
		}
	}

}
