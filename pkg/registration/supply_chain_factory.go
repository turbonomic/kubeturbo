package registration

import (
	"fmt"

	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"

	"github.com/golang/glog"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.com/turbonomic/turbo-go-sdk/pkg/supplychain"
)

var (
	vCpuType          proto.CommodityDTO_CommodityType = proto.CommodityDTO_VCPU
	vMemType          proto.CommodityDTO_CommodityType = proto.CommodityDTO_VMEM
	cpuAllocationType proto.CommodityDTO_CommodityType = proto.CommodityDTO_CPU_ALLOCATION
	memAllocationType proto.CommodityDTO_CommodityType = proto.CommodityDTO_MEM_ALLOCATION
	clusterType       proto.CommodityDTO_CommodityType = proto.CommodityDTO_CLUSTER
	vmPMAccessType    proto.CommodityDTO_CommodityType = proto.CommodityDTO_VMPM_ACCESS
	appCommType       proto.CommodityDTO_CommodityType = proto.CommodityDTO_APPLICATION
	transactionType   proto.CommodityDTO_CommodityType = proto.CommodityDTO_TRANSACTION

	fakeKey string = "fake"

	vCpuTemplateComm                 *proto.TemplateCommodity = &proto.TemplateCommodity{CommodityType: &vCpuType}
	vMemTemplateComm                 *proto.TemplateCommodity = &proto.TemplateCommodity{CommodityType: &vMemType}
	cpuAllocationTemplateCommWithKey *proto.TemplateCommodity = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &cpuAllocationType}
	memAllocationTemplateCommWithKey *proto.TemplateCommodity = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &memAllocationType}
	cpuAllocationTemplateComm        *proto.TemplateCommodity = &proto.TemplateCommodity{CommodityType: &cpuAllocationType}
	memAllocationTemplateComm        *proto.TemplateCommodity = &proto.TemplateCommodity{CommodityType: &memAllocationType}
	clusterTemplateComm              *proto.TemplateCommodity = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &clusterType}
	vmpmAccessTemplateComm           *proto.TemplateCommodity = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &vmPMAccessType}
	applicationTemplateComm          *proto.TemplateCommodity = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &appCommType}
	transactionTemplateComm          *proto.TemplateCommodity = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &transactionType}
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
	// Node supply chain template
	nodeSupplyChainNode, err := f.buildNodeSupplyBuilder()
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("supply chain node : %++v", nodeSupplyChainNode)

	// Resource Quota supply chain template
	quotaSupplyChainNode, err := f.buildQuotaSupplyBuilder()
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("supply chain node : %++v", quotaSupplyChainNode)

	// Pod supply chain template
	podSupplyChainNode, err := f.buildPodSupplyBuilder()
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("supply chain node : %++v", podSupplyChainNode)

	// Container supply chain template
	containerSupplyChainNode, err := f.buildContainer()
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("supply chain node : %++v", containerSupplyChainNode)

	// Application supply chain template
	appSupplyChainNode, err := f.buildApplicationSupplyBuilder()
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("supply chain node : %++v", appSupplyChainNode)

	// Virtual application supply chain template
	vAppSupplyChainNode, err := f.buildVirtualApplicationSupplyBuilder()
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("supply chain node : %++v", vAppSupplyChainNode)

	supplyChainBuilder := supplychain.NewSupplyChainBuilder()
	supplyChainBuilder.Top(vAppSupplyChainNode)
	supplyChainBuilder.Entity(appSupplyChainNode)
	supplyChainBuilder.Entity(containerSupplyChainNode)
	supplyChainBuilder.Entity(podSupplyChainNode)
	supplyChainBuilder.Entity(quotaSupplyChainNode)
	supplyChainBuilder.Entity(nodeSupplyChainNode)

	return supplyChainBuilder.Create()
}

func (f *SupplyChainFactory) buildNodeSupplyBuilder() (*proto.TemplateDTO, error) {
	nodeSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_VIRTUAL_MACHINE)

	nodeSupplyChainNodeBuilder = nodeSupplyChainNodeBuilder.
		Sells(vCpuTemplateComm). // sells to Pods
		Sells(vMemTemplateComm). // sells to Pods
		// also sells VMPMAccess, Cluster to Pods
		Sells(cpuAllocationTemplateCommWithKey). //sells to Quotas
		Sells(memAllocationTemplateCommWithKey)  //sells to Quotas

	return nodeSupplyChainNodeBuilder.Create()
}

func (f *SupplyChainFactory) buildQuotaSupplyBuilder() (*proto.TemplateDTO, error) {
	nodeSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_VIRTUAL_DATACENTER)
	nodeSupplyChainNodeBuilder = nodeSupplyChainNodeBuilder.
		Sells(cpuAllocationTemplateComm).
		Sells(memAllocationTemplateComm).
		Provider(proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_LAYERED_OVER).
		Buys(cpuAllocationTemplateCommWithKey).
		Buys(memAllocationTemplateCommWithKey)

	// Link from Quota to VM
	vmQuotaExtLinkBuilder := supplychain.NewExternalEntityLinkBuilder()
	vmQuotaExtLinkBuilder.Link(proto.EntityDTO_VIRTUAL_DATACENTER, proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_LAYERED_OVER).
		Commodity(cpuAllocationType, true).
		Commodity(memAllocationType, true)

	err := f.addVMStitchingProperty(vmQuotaExtLinkBuilder)
	if err != nil {
		return nil, err
	}

	vmQuotaExternalLink, err := vmQuotaExtLinkBuilder.Build()
	if err != nil {
		return nil, err
	}

	return nodeSupplyChainNodeBuilder.ConnectsTo(vmQuotaExternalLink).Create()
}

func (f *SupplyChainFactory) buildPodSupplyBuilder() (*proto.TemplateDTO, error) {
	podSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_CONTAINER_POD)
	podSupplyChainNodeBuilder = podSupplyChainNodeBuilder.
		Sells(vCpuTemplateComm). //sells to containers
		Sells(vMemTemplateComm).
		Sells(vmpmAccessTemplateComm).
		Provider(proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_HOSTING).
		Buys(vCpuTemplateComm).
		Buys(vMemTemplateComm).
		Provider(proto.EntityDTO_VIRTUAL_DATACENTER, proto.Provider_LAYERED_OVER).
		Buys(cpuAllocationTemplateComm).
		Buys(memAllocationTemplateComm)

	// Link from Pod to VM
	vmPodExtLinkBuilder := supplychain.NewExternalEntityLinkBuilder()
	vmPodExtLinkBuilder.Link(proto.EntityDTO_CONTAINER_POD, proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_HOSTING).
		Commodity(vCpuType, false).
		Commodity(vMemType, false).
		Commodity(vmPMAccessType, true).
		Commodity(clusterType, true)

	err := f.addVMStitchingProperty(vmPodExtLinkBuilder)
	if err != nil {
		return nil, err
	}

	vmPodExternalLink, err := vmPodExtLinkBuilder.Build()
	if err != nil {
		return nil, err
	}
	return podSupplyChainNodeBuilder.ConnectsTo(vmPodExternalLink).Create()
}

func (f *SupplyChainFactory) addVMStitchingProperty(extLinkBuilder *supplychain.ExternalEntityLinkBuilder) error {
	switch f.stitchingPropertyType {
	case stitching.UUID:
		extLinkBuilder.
			ProbeEntityPropertyDef(supplychain.SUPPLY_CHAIN_CONSTANT_UUID, "UUID of the Node").
			ExternalEntityPropertyDef(supplychain.VM_UUID)
	case stitching.IP:
		extLinkBuilder.
			ProbeEntityPropertyDef(supplychain.SUPPLY_CHAIN_CONSTANT_IP_ADDRESS, "IP of the Node").
			ExternalEntityPropertyDef(supplychain.VM_IP)
	default:
		return fmt.Errorf("Stitching property type %s is not supported.", f.stitchingPropertyType)
	}
	return nil
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
