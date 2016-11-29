package externalprobebuilder

import (
	// comm "github.com/vmturbo/vmturbo-go-sdk/communicator"
	"github.com/vmturbo/vmturbo-go-sdk/pkg/builder"
	"github.com/vmturbo/vmturbo-go-sdk/pkg/common"
	"github.com/vmturbo/vmturbo-go-sdk/pkg/proto"
)

var (
	vCpuType           proto.CommodityDTO_CommodityType = proto.CommodityDTO_VCPU
	vMemType           proto.CommodityDTO_CommodityType = proto.CommodityDTO_VMEM
	cpuProvisionedType proto.CommodityDTO_CommodityType = proto.CommodityDTO_CPU_PROVISIONED
	memProvisionedType proto.CommodityDTO_CommodityType = proto.CommodityDTO_MEM_PROVISIONED
	cpuAllocationType  proto.CommodityDTO_CommodityType = proto.CommodityDTO_CPU_ALLOCATION
	memAllocationType  proto.CommodityDTO_CommodityType = proto.CommodityDTO_MEM_ALLOCATION
	transactionType    proto.CommodityDTO_CommodityType = proto.CommodityDTO_TRANSACTION

	clusterType    proto.CommodityDTO_CommodityType = proto.CommodityDTO_CLUSTER
	appCommType    proto.CommodityDTO_CommodityType = proto.CommodityDTO_APPLICATION
	vmpmaccessType proto.CommodityDTO_CommodityType = proto.CommodityDTO_VMPM_ACCESS

	fakeKey  string = "fake"
	emptyKey string = ""

	vCpuTemplateComm           proto.TemplateCommodity = proto.TemplateCommodity{CommodityType: &vCpuType}
	vMemTemplateComm           proto.TemplateCommodity = proto.TemplateCommodity{CommodityType: &vMemType}
	cpuAllocationTemplateComm  proto.TemplateCommodity = proto.TemplateCommodity{Key: &fakeKey, CommodityType: &cpuAllocationType}
	memAllocationTemplateComm  proto.TemplateCommodity = proto.TemplateCommodity{Key: &fakeKey, CommodityType: &memAllocationType}
	cpuProvisionedTemplateComm proto.TemplateCommodity = proto.TemplateCommodity{Key: &fakeKey, CommodityType: &cpuProvisionedType}
	memProvisionedTemplateComm proto.TemplateCommodity = proto.TemplateCommodity{Key: &fakeKey, CommodityType: &memProvisionedType}
	applicationTemplateComm    proto.TemplateCommodity = proto.TemplateCommodity{Key: &fakeKey, CommodityType: &appCommType}
	clusterTemplateComm        proto.TemplateCommodity = proto.TemplateCommodity{Key: &fakeKey, CommodityType: &clusterType}
	transactionTemplateComm    proto.TemplateCommodity = proto.TemplateCommodity{Key: &fakeKey, CommodityType: &transactionType}
)

type SupplyChainFactory struct{}

func (this *SupplyChainFactory) CreateSupplyChain() ([]*proto.TemplateDTO, error) {

	nodeSupplyChainNodeBuilder := this.nodeSupplyBuilder()

	// Pod Supplychain builder
	podSupplyChainNodeBuilder := this.podSupplyBuilder()

	// Application supplychain builder
	appSupplyChainNodeBuilder := this.applicationSupplyBuilder()

	// Virtual Application supplychain builder
	vAppSupplyChainNodeBuilder := this.vitualApplicationSupplyBuilder()

	// Link from Pod to VM
	vmPodExtLinkBuilder := builder.NewExternalEntityLinkBuilder()
	vmPodExtLinkBuilder.Link(proto.EntityDTO_CONTAINER_POD, proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_LAYERED_OVER).
		Commodity(cpuAllocationType, true).
		Commodity(memAllocationType, true).
		Commodity(cpuProvisionedType, true).
		Commodity(memProvisionedType, true).
		Commodity(vmpmaccessType, true).
		Commodity(clusterType, true).
		ProbeEntityPropertyDef(common.SUPPLYCHAIN_CONSTANT_IP_ADDRESS, "IP Address where the Pod is running").
		ExternalEntityPropertyDef(common.VM_IP)
	vmPodExternalLink := vmPodExtLinkBuilder.Build()

	// Link from Application to VM
	vmAppExtLinkBuilder := builder.NewExternalEntityLinkBuilder()
	vmAppExtLinkBuilder.Link(proto.EntityDTO_APPLICATION, proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_HOSTING).
		Commodity(vCpuType, false).
		Commodity(vMemType, false).
		Commodity(appCommType, true).
		ProbeEntityPropertyDef(common.SUPPLYCHAIN_CONSTANT_IP_ADDRESS, "IP Address where the Application is running").
		ExternalEntityPropertyDef(common.VM_IP)
	vmAppExternalLink := vmAppExtLinkBuilder.Build()

	supplyChainBuilder := builder.NewSupplyChainBuilder()
	supplyChainBuilder.Top(vAppSupplyChainNodeBuilder)
	supplyChainBuilder.Entity(appSupplyChainNodeBuilder)
	supplyChainBuilder.ConnectsTo(vmAppExternalLink)
	supplyChainBuilder.Entity(podSupplyChainNodeBuilder)
	supplyChainBuilder.ConnectsTo(vmPodExternalLink)
	supplyChainBuilder.Entity(nodeSupplyChainNodeBuilder)

	return supplyChainBuilder.Create()
}

func (this *SupplyChainFactory) nodeSupplyBuilder() *builder.SupplyChainNodeBuilder {
	nodeSupplyChainNodeBuilder := builder.NewSupplyChainNodeBuilder()
	nodeSupplyChainNodeBuilder = nodeSupplyChainNodeBuilder.
		Entity(proto.EntityDTO_VIRTUAL_MACHINE).
		Sells(vCpuTemplateComm).
		Sells(vMemTemplateComm).
		Sells(cpuProvisionedTemplateComm).
		Sells(memProvisionedTemplateComm).
		Sells(cpuAllocationTemplateComm).
		Sells(memAllocationTemplateComm).
		Sells(applicationTemplateComm).
		Sells(clusterTemplateComm)

	return nodeSupplyChainNodeBuilder
}

func (this *SupplyChainFactory) podSupplyBuilder() *builder.SupplyChainNodeBuilder {
	// Pod Supplychain builder
	podSupplyChainNodeBuilder := builder.NewSupplyChainNodeBuilder()
	podSupplyChainNodeBuilder = podSupplyChainNodeBuilder.
		Entity(proto.EntityDTO_CONTAINER_POD).
		Sells(cpuAllocationTemplateComm).
		Sells(memAllocationTemplateComm).
		Provider(proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_LAYERED_OVER).
		Buys(cpuProvisionedTemplateComm).
		Buys(memProvisionedTemplateComm).
		Buys(cpuAllocationTemplateComm).
		Buys(memAllocationTemplateComm).
		Buys(clusterTemplateComm)

	return podSupplyChainNodeBuilder
}

func (this *SupplyChainFactory) applicationSupplyBuilder() *builder.SupplyChainNodeBuilder {
	// Application supplychain builder
	appSupplyChainNodeBuilder := builder.NewSupplyChainNodeBuilder()
	appSupplyChainNodeBuilder = appSupplyChainNodeBuilder.
		Entity(proto.EntityDTO_APPLICATION).
		Sells(transactionTemplateComm).
		Provider(proto.EntityDTO_CONTAINER_POD, proto.Provider_LAYERED_OVER). // Buys CpuAllocation and MemAllocation from Pod
		Buys(cpuAllocationTemplateComm).
		Buys(memAllocationTemplateComm).
		Provider(proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_HOSTING). // Buys VCpu, VMem and ApplicationCommodity from VM
		Buys(vCpuTemplateComm).
		Buys(vMemTemplateComm).
		Buys(applicationTemplateComm)
	return appSupplyChainNodeBuilder
}

func (this *SupplyChainFactory) vitualApplicationSupplyBuilder() *builder.SupplyChainNodeBuilder {
	vAppSupplyChainNodeBuilder := builder.NewSupplyChainNodeBuilder()
	vAppSupplyChainNodeBuilder = vAppSupplyChainNodeBuilder.
		Entity(proto.EntityDTO_VIRTUAL_APPLICATION).
		Provider(proto.EntityDTO_APPLICATION, proto.Provider_LAYERED_OVER).
		Buys(transactionTemplateComm)
	return vAppSupplyChainNodeBuilder
}
