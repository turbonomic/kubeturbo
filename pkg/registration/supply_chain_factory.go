package registration

import (
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.com/turbonomic/turbo-go-sdk/pkg/supplychain"
)

var (
	vCpuType           proto.CommodityDTO_CommodityType = proto.CommodityDTO_VCPU
	vMemType           proto.CommodityDTO_CommodityType = proto.CommodityDTO_VMEM
	cpuProvisionedType proto.CommodityDTO_CommodityType = proto.CommodityDTO_CPU_PROVISIONED
	memProvisionedType proto.CommodityDTO_CommodityType = proto.CommodityDTO_MEM_PROVISIONED
	transactionType    proto.CommodityDTO_CommodityType = proto.CommodityDTO_TRANSACTION

	clusterType    proto.CommodityDTO_CommodityType = proto.CommodityDTO_CLUSTER
	appCommType    proto.CommodityDTO_CommodityType = proto.CommodityDTO_APPLICATION
	vmPMAccessType proto.CommodityDTO_CommodityType = proto.CommodityDTO_VMPM_ACCESS

	fakeKey string = "fake"

	vCpuTemplateComm           *proto.TemplateCommodity = &proto.TemplateCommodity{CommodityType: &vCpuType}
	vMemTemplateComm           *proto.TemplateCommodity = &proto.TemplateCommodity{CommodityType: &vMemType}
	cpuProvisionedTemplateComm *proto.TemplateCommodity = &proto.TemplateCommodity{CommodityType: &cpuProvisionedType}
	memProvisionedTemplateComm *proto.TemplateCommodity = &proto.TemplateCommodity{CommodityType: &memProvisionedType}
	applicationTemplateComm    *proto.TemplateCommodity = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &appCommType}
	clusterTemplateComm        *proto.TemplateCommodity = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &clusterType}
	transactionTemplateComm    *proto.TemplateCommodity = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &transactionType}
)

type SupplyChainFactory struct{}

func NewSupplyChainFactory() *SupplyChainFactory {
	return &SupplyChainFactory{}
}

func (f *SupplyChainFactory) createSupplyChain() ([]*proto.TemplateDTO, error) {
	// Node supply chain builder
	nodeSupplyChainNodeBuilder, err := f.buildNodeSupplyBuilder()
	if err != nil {
		return nil, err
	}

	// Pod supply chain builder
	podSupplyChainNodeBuilder, err := f.buildPodSupplyBuilder()
	if err != nil {
		return nil, err
	}

	// Application supply chain builder
	appSupplyChainNodeBuilder, err := f.buildApplicationSupplyBuilder()
	if err != nil {
		return nil, err
	}

	// Virtual application supply chain builder
	vAppSupplyChainNodeBuilder, err := f.buildVirtualApplicationSupplyBuilder()
	if err != nil {
		return nil, err
	}

	supplyChainBuilder := supplychain.NewSupplyChainBuilder()
	supplyChainBuilder.Top(vAppSupplyChainNodeBuilder)
	supplyChainBuilder.Entity(appSupplyChainNodeBuilder)
	supplyChainBuilder.Entity(podSupplyChainNodeBuilder)
	supplyChainBuilder.Entity(nodeSupplyChainNodeBuilder)

	return supplyChainBuilder.Create()
}

func (f *SupplyChainFactory) buildNodeSupplyBuilder() (*proto.TemplateDTO, error) {
	nodeSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_VIRTUAL_MACHINE)
	nodeSupplyChainNodeBuilder = nodeSupplyChainNodeBuilder.
		Sells(vCpuTemplateComm).
		Sells(vMemTemplateComm).
		Sells(cpuProvisionedTemplateComm).
		Sells(memProvisionedTemplateComm).
		Sells(applicationTemplateComm).
		Sells(clusterTemplateComm)

	return nodeSupplyChainNodeBuilder.Create()
}

func (f *SupplyChainFactory) buildPodSupplyBuilder() (*proto.TemplateDTO, error) {
	// Pod supply chain node builder
	podSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_CONTAINER_POD)
	podSupplyChainNodeBuilder = podSupplyChainNodeBuilder.
		Sells(vCpuTemplateComm).
		Sells(vMemTemplateComm).
		Sells(applicationTemplateComm).
		Provider(proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_HOSTING).
		Buys(cpuProvisionedTemplateComm).
		Buys(memProvisionedTemplateComm).
		Buys(clusterTemplateComm)

	// Link from Pod to VM
	vmPodExtLinkBuilder := supplychain.NewExternalEntityLinkBuilder()
	vmPodExtLinkBuilder.Link(proto.EntityDTO_CONTAINER_POD, proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_HOSTING).
		Commodity(vCpuType, false).
		Commodity(vMemType, false).
		Commodity(cpuProvisionedType, false).
		Commodity(memProvisionedType, false).
		Commodity(vmPMAccessType, true).
		Commodity(clusterType, true).
		ProbeEntityPropertyDef(supplychain.SUPPLY_CHAIN_CONSTANT_IP_ADDRESS, "IP Address where the Pod is running").
		ExternalEntityPropertyDef(supplychain.VM_IP)
	vmPodExternalLink, err := vmPodExtLinkBuilder.Build()
	if err != nil {
		return nil, err
	}

	return podSupplyChainNodeBuilder.ConnectsTo(vmPodExternalLink).Create()
}

func (f *SupplyChainFactory) buildApplicationSupplyBuilder() (*proto.TemplateDTO, error) {
	// Application supply chain builder
	appSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_APPLICATION)
	appSupplyChainNodeBuilder = appSupplyChainNodeBuilder.
		Sells(transactionTemplateComm).
		Provider(proto.EntityDTO_CONTAINER_POD, proto.Provider_HOSTING).
		Buys(vCpuTemplateComm).
		Buys(vMemTemplateComm).
		Buys(applicationTemplateComm)

	return appSupplyChainNodeBuilder.Create()
}

func (f *SupplyChainFactory) buildVirtualApplicationSupplyBuilder() (*proto.TemplateDTO, error) {
	vAppSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_VIRTUAL_APPLICATION)
	vAppSupplyChainNodeBuilder = vAppSupplyChainNodeBuilder.
		Provider(proto.EntityDTO_APPLICATION, proto.Provider_HOSTING).
		Buys(transactionTemplateComm)
	return vAppSupplyChainNodeBuilder.Create()
}
