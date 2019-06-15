package registration

import (
	"fmt"

	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"

	"github.com/golang/glog"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.com/turbonomic/turbo-go-sdk/pkg/supplychain"
)

var (
	vCpuType                 = proto.CommodityDTO_VCPU
	vMemType                 = proto.CommodityDTO_VMEM
	vCpuRequestType          = proto.CommodityDTO_VCPU_REQUEST
	vMemRequestType          = proto.CommodityDTO_VMEM_REQUEST
	cpuAllocationType        = proto.CommodityDTO_CPU_ALLOCATION
	memAllocationType        = proto.CommodityDTO_MEM_ALLOCATION
	cpuRequestAllocationType = proto.CommodityDTO_CPU_REQUEST_ALLOCATION
	memRequestAllocationType = proto.CommodityDTO_MEM_REQUEST_ALLOCATION
	clusterType              = proto.CommodityDTO_CLUSTER
	vmPMAccessType           = proto.CommodityDTO_VMPM_ACCESS
	appCommType              = proto.CommodityDTO_APPLICATION

	fakeKey = "fake"

	vCpuTemplateComm                        = &proto.TemplateCommodity{CommodityType: &vCpuType}
	vMemTemplateComm                        = &proto.TemplateCommodity{CommodityType: &vMemType}
	vCpuRequestTemplateComm                 = &proto.TemplateCommodity{CommodityType: &vCpuRequestType}
	vMemRequestTemplateComm                 = &proto.TemplateCommodity{CommodityType: &vMemRequestType}
	cpuAllocationTemplateCommWithKey        = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &cpuAllocationType}
	memAllocationTemplateCommWithKey        = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &memAllocationType}
	cpuRequestAllocationTemplateCommWithKey = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &cpuRequestAllocationType}
	memRequestAllocationTemplateCommWithKey = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &memRequestAllocationType}
	vmpmAccessTemplateComm                  = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &vmPMAccessType}
	applicationTemplateCommWithKey          = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &appCommType}
)

type SupplyChainFactory struct {
	// The property used for stitching.
	stitchingPropertyType stitching.StitchingPropertyType
	vmPriority            int32
	vmTemplateType        proto.TemplateDTO_TemplateType
}

func NewSupplyChainFactory(pType stitching.StitchingPropertyType, vmPriority int32, base bool) *SupplyChainFactory {
	tmptype := proto.TemplateDTO_EXTENSION
	if base {
		tmptype = proto.TemplateDTO_BASE
	}
	return &SupplyChainFactory{
		stitchingPropertyType: pType,
		vmPriority:            vmPriority,
		vmTemplateType:        tmptype,
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
	nodeSupplyChainNodeBuilder.SetPriority(f.vmPriority)
	nodeSupplyChainNodeBuilder.SetTemplateType(f.vmTemplateType)

	nodeSupplyChainNodeBuilder = nodeSupplyChainNodeBuilder.
		Sells(vCpuTemplateComm).        // sells to Pods
		Sells(vMemTemplateComm).        // sells to Pods
		Sells(vCpuRequestTemplateComm). // sells to Pods
		Sells(vMemRequestTemplateComm). // sells to Pods
		Sells(vmpmAccessTemplateComm).  // sells to Pods
		// also sells Cluster to Pods
		Sells(cpuAllocationTemplateCommWithKey).        //sells to Quotas
		Sells(memAllocationTemplateCommWithKey).        //sells to Quotas
		Sells(cpuRequestAllocationTemplateCommWithKey). //sells to Quotas
		Sells(memRequestAllocationTemplateCommWithKey)  //sells to Quotas

	return nodeSupplyChainNodeBuilder.Create()
}

func (f *SupplyChainFactory) buildQuotaSupplyBuilder() (*proto.TemplateDTO, error) {
	nodeSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_VIRTUAL_DATACENTER)
	nodeSupplyChainNodeBuilder = nodeSupplyChainNodeBuilder.
		Sells(cpuAllocationTemplateCommWithKey).
		Sells(memAllocationTemplateCommWithKey).
		Sells(cpuRequestAllocationTemplateCommWithKey).
		Sells(memRequestAllocationTemplateCommWithKey).
		Provider(proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_LAYERED_OVER).
		Buys(cpuAllocationTemplateCommWithKey).
		Buys(memAllocationTemplateCommWithKey).
		Buys(cpuRequestAllocationTemplateCommWithKey).
		Buys(memRequestAllocationTemplateCommWithKey)

	// Link from Quota to VM
	vmQuotaExtLinkBuilder := supplychain.NewExternalEntityLinkBuilder()
	vmQuotaExtLinkBuilder.Link(proto.EntityDTO_VIRTUAL_DATACENTER, proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_LAYERED_OVER).
		Commodity(cpuAllocationType, true).
		Commodity(memAllocationType, true).
		Commodity(cpuRequestAllocationType, true).
		Commodity(memRequestAllocationType, true)

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
		Buys(vCpuRequestTemplateComm).
		Buys(vMemRequestTemplateComm).
		Provider(proto.EntityDTO_VIRTUAL_DATACENTER, proto.Provider_LAYERED_OVER).
		Buys(cpuAllocationTemplateCommWithKey).
		Buys(memAllocationTemplateCommWithKey).
		Buys(cpuRequestAllocationTemplateCommWithKey).
		Buys(memRequestAllocationTemplateCommWithKey)

	// Link from Pod to VM
	vmPodExtLinkBuilder := supplychain.NewExternalEntityLinkBuilder()
	vmPodExtLinkBuilder.Link(proto.EntityDTO_CONTAINER_POD, proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_HOSTING).
		Commodity(vCpuType, false).
		Commodity(vMemType, false).
		Commodity(vCpuRequestType, false).
		Commodity(vMemRequestType, false).
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
		return fmt.Errorf("stitching property type %s is not supported", f.stitchingPropertyType)
	}
	return nil
}

func (f *SupplyChainFactory) buildContainer() (*proto.TemplateDTO, error) {
	builder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_CONTAINER).
		Sells(vCpuTemplateComm).
		Sells(vMemTemplateComm).
		Sells(applicationTemplateCommWithKey).
		Provider(proto.EntityDTO_CONTAINER_POD, proto.Provider_HOSTING).
		Buys(vCpuTemplateComm).
		Buys(vMemTemplateComm).
		Buys(vmpmAccessTemplateComm)

	return builder.Create()
}

func (f *SupplyChainFactory) buildApplicationSupplyBuilder() (*proto.TemplateDTO, error) {
	appSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_APPLICATION)
	appSupplyChainNodeBuilder = appSupplyChainNodeBuilder.
		Sells(applicationTemplateCommWithKey). // The key used to sell to the virtual applications
		Provider(proto.EntityDTO_CONTAINER, proto.Provider_HOSTING).
		Buys(vCpuTemplateComm).
		Buys(vMemTemplateComm).
		Buys(applicationTemplateCommWithKey) // The key used to buy from the container

	return appSupplyChainNodeBuilder.Create()
}

func (f *SupplyChainFactory) buildVirtualApplicationSupplyBuilder() (*proto.TemplateDTO, error) {
	vAppSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_VIRTUAL_APPLICATION)
	vAppSupplyChainNodeBuilder = vAppSupplyChainNodeBuilder.
		Provider(proto.EntityDTO_APPLICATION, proto.Provider_LAYERED_OVER).
		Buys(applicationTemplateCommWithKey)
	return vAppSupplyChainNodeBuilder.Create()
}
