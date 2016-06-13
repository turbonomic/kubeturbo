package externalprobebuilder

import (
	// comm "github.com/vmturbo/vmturbo-go-sdk/communicator"
	"github.com/vmturbo/vmturbo-go-sdk/sdk"
)

var (
	vCpuType          sdk.CommodityDTO_CommodityType = sdk.CommodityDTO_VCPU
	vMemType          sdk.CommodityDTO_CommodityType = sdk.CommodityDTO_VMEM
	cpuAllocationType sdk.CommodityDTO_CommodityType = sdk.CommodityDTO_CPU_ALLOCATION
	memAllocationType sdk.CommodityDTO_CommodityType = sdk.CommodityDTO_MEM_ALLOCATION
	transactionType   sdk.CommodityDTO_CommodityType = sdk.CommodityDTO_TRANSACTION

	clusterType    sdk.CommodityDTO_CommodityType = sdk.CommodityDTO_CLUSTER
	appCommType    sdk.CommodityDTO_CommodityType = sdk.CommodityDTO_APPLICATION
	vmpmaccessType sdk.CommodityDTO_CommodityType = sdk.CommodityDTO_VMPM_ACCESS

	fakeKey  string = "fake"
	emptyKey string = ""
)

type SupplyChainFactory struct{}

func (this *SupplyChainFactory) CreateSupplyChain() []*sdk.TemplateDTO {

	nodeSupplyChainNodeBuilder := this.nodeSupplyBuilder()

	// Pod Supplychain builder
	podSupplyChainNodeBuilder := this.podSupplyBuilder()

	// Application supplychain builder
	appSupplyChainNodeBuilder := this.applicationSupplyBuilder()

	// Virtual Application supplychain builder
	vAppSupplyChainNodeBuilder := this.vitualApplicationSupplyBuilder()

	// Link from Pod to VM
	vmPodExtLinkBuilder := sdk.NewExternalEntityLinkBuilder()
	vmPodExtLinkBuilder.Link(sdk.EntityDTO_CONTAINER_POD, sdk.EntityDTO_VIRTUAL_MACHINE, sdk.Provider_LAYERED_OVER).
		Commodity(cpuAllocationType, true).
		Commodity(memAllocationType, true).
		Commodity(vmpmaccessType, true).
		Commodity(clusterType, true).
		ProbeEntityPropertyDef(sdk.SUPPLYCHAIN_CONSTANT_IP_ADDRESS, "IP Address where the Pod is running").
		ExternalEntityPropertyDef(sdk.VM_IP)
	vmPodExternalLink := vmPodExtLinkBuilder.Build()

	// Link from Application to VM
	vmAppExtLinkBuilder := sdk.NewExternalEntityLinkBuilder()
	vmAppExtLinkBuilder.Link(sdk.EntityDTO_APPLICATION, sdk.EntityDTO_VIRTUAL_MACHINE, sdk.Provider_HOSTING).
		Commodity(vCpuType, false).
		Commodity(vMemType, false).
		Commodity(appCommType, true).
		ProbeEntityPropertyDef(sdk.SUPPLYCHAIN_CONSTANT_IP_ADDRESS, "IP Address where the Application is running").
		ExternalEntityPropertyDef(sdk.VM_IP)
	vmAppExternalLink := vmAppExtLinkBuilder.Build()

	supplyChainBuilder := sdk.NewSupplyChainBuilder()
	supplyChainBuilder.Top(vAppSupplyChainNodeBuilder)
	supplyChainBuilder.Entity(appSupplyChainNodeBuilder)
	supplyChainBuilder.ConnectsTo(vmAppExternalLink)
	supplyChainBuilder.Entity(podSupplyChainNodeBuilder)
	supplyChainBuilder.ConnectsTo(vmPodExternalLink)
	supplyChainBuilder.Entity(nodeSupplyChainNodeBuilder)

	return supplyChainBuilder.Create()
}

func (this *SupplyChainFactory) nodeSupplyBuilder() *sdk.SupplyChainNodeBuilder {
	nodeSupplyChainNodeBuilder := sdk.NewSupplyChainNodeBuilder()
	nodeSupplyChainNodeBuilder = nodeSupplyChainNodeBuilder.
		Entity(sdk.EntityDTO_VIRTUAL_MACHINE).
		Selling(sdk.CommodityDTO_CPU_ALLOCATION, fakeKey).
		Selling(sdk.CommodityDTO_MEM_ALLOCATION, fakeKey).
		Selling(sdk.CommodityDTO_VMPM_ACCESS, fakeKey).
		Selling(sdk.CommodityDTO_VCPU, emptyKey).
		Selling(sdk.CommodityDTO_VMEM, emptyKey).
		Selling(sdk.CommodityDTO_APPLICATION, fakeKey).
		Selling(sdk.CommodityDTO_CLUSTER, fakeKey)

	return nodeSupplyChainNodeBuilder
}

func (this *SupplyChainFactory) podSupplyBuilder() *sdk.SupplyChainNodeBuilder {
	// Pod Supplychain builder
	podSupplyChainNodeBuilder := sdk.NewSupplyChainNodeBuilder()
	podSupplyChainNodeBuilder = podSupplyChainNodeBuilder.
		Entity(sdk.EntityDTO_CONTAINER_POD).
		Selling(sdk.CommodityDTO_CPU_ALLOCATION, fakeKey).
		Selling(sdk.CommodityDTO_MEM_ALLOCATION, fakeKey)

	cpuAllocationTemplateComm := &sdk.TemplateCommodity{
		Key:           &fakeKey,
		CommodityType: &cpuAllocationType,
	}
	memAllocationTemplateComm := &sdk.TemplateCommodity{
		Key:           &fakeKey,
		CommodityType: &memAllocationType,
	}
	clusterTemplateComm := &sdk.TemplateCommodity{
		Key:           &fakeKey,
		CommodityType: &clusterType,
	}

	podSupplyChainNodeBuilder = podSupplyChainNodeBuilder.
		Provider(sdk.EntityDTO_VIRTUAL_MACHINE, sdk.Provider_LAYERED_OVER).
		Buys(*cpuAllocationTemplateComm).
		Buys(*memAllocationTemplateComm).
		Buys(*clusterTemplateComm)

	return podSupplyChainNodeBuilder
}

func (this *SupplyChainFactory) applicationSupplyBuilder() *sdk.SupplyChainNodeBuilder {
	// Application supplychain builder
	appSupplyChainNodeBuilder := sdk.NewSupplyChainNodeBuilder()
	appSupplyChainNodeBuilder = appSupplyChainNodeBuilder.
		Entity(sdk.EntityDTO_APPLICATION).
		Selling(sdk.CommodityDTO_TRANSACTION, fakeKey)
	// Buys CpuAllocation and MemAllocation from Pod
	appCpuAllocationTemplateComm := &sdk.TemplateCommodity{
		Key:           &fakeKey,
		CommodityType: &cpuAllocationType,
	}
	appMemAllocationTemplateComm := &sdk.TemplateCommodity{
		Key:           &fakeKey,
		CommodityType: &memAllocationType,
	}
	appSupplyChainNodeBuilder = appSupplyChainNodeBuilder.
		Provider(sdk.EntityDTO_CONTAINER_POD, sdk.Provider_LAYERED_OVER).
		Buys(*appCpuAllocationTemplateComm).
		Buys(*appMemAllocationTemplateComm)
	// Buys VCpu, VMem and ApplicationCommodity from VM
	appVCpu := &sdk.TemplateCommodity{
		CommodityType: &vCpuType,
	}
	appVMem := &sdk.TemplateCommodity{
		CommodityType: &vMemType,
	}
	appAppComm := &sdk.TemplateCommodity{
		Key:           &fakeKey,
		CommodityType: &appCommType,
	}
	appSupplyChainNodeBuilder = appSupplyChainNodeBuilder.
		Provider(sdk.EntityDTO_VIRTUAL_MACHINE, sdk.Provider_HOSTING).
		Buys(*appVCpu).Buys(*appVMem).
		Buys(*appAppComm)
	return appSupplyChainNodeBuilder
}

func (this *SupplyChainFactory) vitualApplicationSupplyBuilder() *sdk.SupplyChainNodeBuilder {
	vAppSupplyChainNodeBuilder := sdk.NewSupplyChainNodeBuilder()
	vAppSupplyChainNodeBuilder = vAppSupplyChainNodeBuilder.
		Entity(sdk.EntityDTO_VIRTUAL_APPLICATION)

	transactionTemplateComm := &sdk.TemplateCommodity{
		Key:           &fakeKey,
		CommodityType: &transactionType,
	}
	vAppSupplyChainNodeBuilder = vAppSupplyChainNodeBuilder.
		Provider(sdk.EntityDTO_APPLICATION, sdk.Provider_LAYERED_OVER).
		Buys(*transactionTemplateComm)
	return vAppSupplyChainNodeBuilder
}
