package registration

import (
	"fmt"

	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.com/turbonomic/turbo-go-sdk/pkg/supplychain"
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
	vmPMAccessType proto.CommodityDTO_CommodityType = proto.CommodityDTO_VMPM_ACCESS

	fakeKey string = "fake"

	vCpuTemplateComm          *proto.TemplateCommodity = &proto.TemplateCommodity{CommodityType: &vCpuType}
	vMemTemplateComm          *proto.TemplateCommodity = &proto.TemplateCommodity{CommodityType: &vMemType}
	cpuAllocationTemplateComm *proto.TemplateCommodity = &proto.TemplateCommodity{CommodityType: &cpuAllocationType}
	memAllocationTemplateComm *proto.TemplateCommodity = &proto.TemplateCommodity{CommodityType: &memAllocationType}
	applicationTemplateComm   *proto.TemplateCommodity = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &appCommType}
	clusterTemplateComm       *proto.TemplateCommodity = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &clusterType}
	transactionTemplateComm   *proto.TemplateCommodity = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &transactionType}
	vmpmAccessTemplateComm    *proto.TemplateCommodity = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &vmPMAccessType}
)

type SupplyChainFactory struct {
	// The property used for stitching.
	stitchingPropertyType stitching.StitchingPropertyType
}

func NewSupplyChainFactory(pType stitching.StitchingPropertyType) *SupplyChainFactory {
	return &SupplyChainFactory{
		stitchingPropertyType: pType,
	}
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

	// Resource Quota template
	quotaSupplyChainNodeBuilder, err := f.buildQuotaSupplyBuilder()
	if err != nil {
		return nil, err
	}

	// Container suplly chain builder
	containerSupplyChainNodeBuilder, err := f.buildContainer()
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
	supplyChainBuilder.Entity(containerSupplyChainNodeBuilder)
	supplyChainBuilder.Entity(podSupplyChainNodeBuilder)
	supplyChainBuilder.Entity(quotaSupplyChainNodeBuilder)
	supplyChainBuilder.Entity(nodeSupplyChainNodeBuilder)

	return supplyChainBuilder.Create()
}

func (f *SupplyChainFactory) buildQuotaSupplyBuilder() (*proto.TemplateDTO, error) {
	nodeSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_VIRTUAL_DATACENTER)
	nodeSupplyChainNodeBuilder = nodeSupplyChainNodeBuilder.
		Sells(cpuAllocationTemplateComm).
		Sells(memAllocationTemplateComm).
		Provider(proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_LAYERED_OVER).
		Buys(cpuAllocationTemplateComm).
		Buys(memAllocationTemplateComm)

	return nodeSupplyChainNodeBuilder.Create()
}

func (f *SupplyChainFactory) buildNodeSupplyBuilder() (*proto.TemplateDTO, error) {
	nodeSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_VIRTUAL_MACHINE)
	nodeSupplyChainNodeBuilder = nodeSupplyChainNodeBuilder.
		Sells(vCpuTemplateComm).
		Sells(vMemTemplateComm).
		Sells(vmpmAccessTemplateComm).
		// TODO we will re-include provisioned commodities sold by node later.
		//Sells(cpuProvisionedTemplateComm).
		//Sells(memProvisionedTemplateComm)
		Sells(cpuAllocationTemplateComm).
		Sells(memAllocationTemplateComm).
		Sells(clusterTemplateComm)

	return nodeSupplyChainNodeBuilder.Create()
}

func (f *SupplyChainFactory) buildPodSupplyBuilder() (*proto.TemplateDTO, error) {
	// Pod supply chain node builder
	podSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_CONTAINER_POD)
	podSupplyChainNodeBuilder = podSupplyChainNodeBuilder.
		Sells(vCpuTemplateComm).
		Sells(vMemTemplateComm).
		Sells(vmpmAccessTemplateComm).
		Provider(proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_HOSTING).
		// TODO we will re-include provisioned commodities bought by pod later.
		//Buys(cpuProvisionedTemplateComm).
		//Buys(memProvisionedTemplateComm).
		Buys(clusterTemplateComm).
		Provider(proto.EntityDTO_VIRTUAL_DATACENTER, proto.Provider_LAYERED_OVER).
		Buys(cpuAllocationTemplateComm).
		Buys(memAllocationTemplateComm)

	// Link from Pod to VM
	vmPodExtLinkBuilder := supplychain.NewExternalEntityLinkBuilder()
	vmPodExtLinkBuilder.Link(proto.EntityDTO_CONTAINER_POD, proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_HOSTING).
		Commodity(vCpuType, false).
		Commodity(vMemType, false).
		//Commodity(cpuProvisionedType, false).
		//Commodity(memProvisionedType, false).
		Commodity(vmPMAccessType, true).
		Commodity(clusterType, true)

	switch f.stitchingPropertyType {
	case stitching.UUID:
		vmPodExtLinkBuilder.
			ProbeEntityPropertyDef(supplychain.SUPPLY_CHAIN_CONSTANT_UUID, "UUID of the Node").
			ExternalEntityPropertyDef(supplychain.VM_UUID)
	case stitching.IP:
		vmPodExtLinkBuilder.
			ProbeEntityPropertyDef(supplychain.SUPPLY_CHAIN_CONSTANT_IP_ADDRESS, "IP of the Node").
			ExternalEntityPropertyDef(supplychain.VM_IP)
	default:
		return nil, fmt.Errorf("Stitching property type %s is not supported.", f.stitchingPropertyType)
	}

	vmPodExternalLink, err := vmPodExtLinkBuilder.Build()
	if err != nil {
		return nil, err
	}

	return podSupplyChainNodeBuilder.ConnectsTo(vmPodExternalLink).Create()
}

func (f *SupplyChainFactory) buildContainer() (*proto.TemplateDTO, error) {
	builder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_CONTAINER).
		Sells(vCpuTemplateComm).
		Sells(vMemTemplateComm).
		Sells(applicationTemplateComm).
		Provider(proto.EntityDTO_CONTAINER_POD, proto.Provider_HOSTING).
		Buys(vCpuTemplateComm).
		Buys(vMemTemplateComm).
		Buys(vmpmAccessTemplateComm)

	return builder.Create()
}

func (f *SupplyChainFactory) buildApplicationSupplyBuilder() (*proto.TemplateDTO, error) {
	// Application supply chain builder
	appSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_APPLICATION)
	appSupplyChainNodeBuilder = appSupplyChainNodeBuilder.
		Sells(transactionTemplateComm).
		Provider(proto.EntityDTO_CONTAINER, proto.Provider_HOSTING).
		Buys(vCpuTemplateComm).
		Buys(vMemTemplateComm).
		Buys(applicationTemplateComm)

	return appSupplyChainNodeBuilder.Create()
}

func (f *SupplyChainFactory) buildVirtualApplicationSupplyBuilder() (*proto.TemplateDTO, error) {
	vAppSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_VIRTUAL_APPLICATION)
	vAppSupplyChainNodeBuilder = vAppSupplyChainNodeBuilder.
		Provider(proto.EntityDTO_APPLICATION, proto.Provider_LAYERED_OVER).
		Buys(transactionTemplateComm)
	return vAppSupplyChainNodeBuilder.Create()
}
