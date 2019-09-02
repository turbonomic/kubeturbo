package registration

import (
	"fmt"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder"

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
	transactionType          = proto.CommodityDTO_TRANSACTION
	respTimeType             = proto.CommodityDTO_RESPONSE_TIME
	numPodNumConsumers       = proto.CommodityDTO_NUMBER_CONSUMERS

	fakeKey = "fake"

	vCpuTemplateComm                        = &proto.TemplateCommodity{CommodityType: &vCpuType}
	vMemTemplateComm                        = &proto.TemplateCommodity{CommodityType: &vMemType}
	vCpuRequestTemplateComm                 = &proto.TemplateCommodity{CommodityType: &vCpuRequestType}
	vMemRequestTemplateComm                 = &proto.TemplateCommodity{CommodityType: &vMemRequestType}
	numPodNumConsumersTemplateComm          = &proto.TemplateCommodity{CommodityType: &numPodNumConsumers}
	cpuAllocationTemplateCommWithKey        = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &cpuAllocationType}
	memAllocationTemplateCommWithKey        = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &memAllocationType}
	cpuRequestAllocationTemplateCommWithKey = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &cpuRequestAllocationType}
	memRequestAllocationTemplateCommWithKey = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &memRequestAllocationType}
	vmpmAccessTemplateComm                  = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &vmPMAccessType}
	applicationTemplateComm                 = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &appCommType}
	transactionTemplateComm                 = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &transactionType}
	respTimeTemplateComm                    = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &respTimeType}

	// Internal matching property
	proxyVMIP   = "Proxy_VM_IP"
	proxyVMUUID = "Proxy_VM_UUID"

	// External matching property
	VMIPFieldName  = supplychain.SUPPLY_CHAIN_CONSTANT_IP_ADDRESS
	VMIPFieldPaths = []string{supplychain.SUPPLY_CHAIN_CONSTANT_VIRTUAL_MACHINE_DATA}
	VMUUID         = supplychain.SUPPLY_CHAIN_CONSTANT_ID
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
	nodeSupplyChainNode.MergedEntityMetaData, err = f.buildNodeMergedEntityMetadata()
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("Supply chain node: %+v", nodeSupplyChainNode)

	// Resource Quota supply chain template
	quotaSupplyChainNode, err := f.buildQuotaSupplyBuilder()
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("Supply chain node: %+v", quotaSupplyChainNode)

	// Pod supply chain template
	podSupplyChainNode, err := f.buildPodSupplyBuilder()
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("Supply chain node: %+v", podSupplyChainNode)

	// Container supply chain template
	containerSupplyChainNode, err := f.buildContainer()
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("Supply chain node: %+v", containerSupplyChainNode)

	// Application supply chain template
	appSupplyChainNode, err := f.buildApplicationSupplyBuilder()
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("Supply chain node: %+v", appSupplyChainNode)

	// Virtual application supply chain template
	vAppSupplyChainNode, err := f.buildVirtualApplicationSupplyBuilder()
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("Supply chain node: %+v", vAppSupplyChainNode)

	supplyChainBuilder := supplychain.NewSupplyChainBuilder()
	supplyChainBuilder.Top(vAppSupplyChainNode)
	supplyChainBuilder.Entity(appSupplyChainNode)
	supplyChainBuilder.Entity(containerSupplyChainNode)
	supplyChainBuilder.Entity(podSupplyChainNode)
	supplyChainBuilder.Entity(quotaSupplyChainNode)
	supplyChainBuilder.Entity(nodeSupplyChainNode)

	return supplyChainBuilder.Create()
}

// Stitching metadata required for stitching with XL
func (f *SupplyChainFactory) buildNodeMergedEntityMetadata() (*proto.MergedEntityMetadata, error) {
	fieldsCapactiy := map[string][]string{
		builder.PropertyCapacity: {},
	}
	fieldsUsedCapacity := map[string][]string{
		builder.PropertyUsed:     {},
		builder.PropertyCapacity: {},
	}
	fieldsUsedCapacityPeak := map[string][]string{
		builder.PropertyUsed:     {},
		builder.PropertyCapacity: {},
		builder.PropertyPeak:     {},
	}
	mergedEntityMetadataBuilder := builder.NewMergedEntityMetadataBuilder()
	// Set up matching criteria based on stitching type
	switch f.stitchingPropertyType {
	case stitching.UUID:
		mergedEntityMetadataBuilder.
			InternalMatchingType(builder.MergedEntityMetadata_STRING).
			InternalMatchingProperty(proxyVMUUID).
			ExternalMatchingType(builder.MergedEntityMetadata_STRING).
			ExternalMatchingField(VMUUID, []string{})
	case stitching.IP:
		mergedEntityMetadataBuilder.
			InternalMatchingType(builder.MergedEntityMetadata_LIST_STRING).
			InternalMatchingPropertyWithDelimiter(proxyVMIP, ",").
			ExternalMatchingType(builder.MergedEntityMetadata_LIST_STRING).
			ExternalMatchingFieldWithDelimiter(VMIPFieldName, VMIPFieldPaths, ",")
	default:
		return nil, fmt.Errorf("stitching property type %s is not supported",
			f.stitchingPropertyType)
	}
	return mergedEntityMetadataBuilder.
		PatchSoldMetadata(proto.CommodityDTO_CLUSTER, fieldsCapactiy).
		PatchSoldMetadata(proto.CommodityDTO_VMPM_ACCESS, fieldsCapactiy).
		PatchSoldMetadata(proto.CommodityDTO_VCPU, fieldsUsedCapacityPeak).
		PatchSoldMetadata(proto.CommodityDTO_VMEM, fieldsUsedCapacityPeak).
		PatchSoldMetadata(proto.CommodityDTO_VCPU_REQUEST, fieldsUsedCapacity).
		PatchSoldMetadata(proto.CommodityDTO_VMEM_REQUEST, fieldsUsedCapacity).
		PatchSoldMetadata(proto.CommodityDTO_CPU_ALLOCATION, fieldsUsedCapacity).
		PatchSoldMetadata(proto.CommodityDTO_MEM_ALLOCATION, fieldsUsedCapacity).
		PatchSoldMetadata(proto.CommodityDTO_CPU_REQUEST_ALLOCATION, fieldsUsedCapacity).
		PatchSoldMetadata(proto.CommodityDTO_MEM_REQUEST_ALLOCATION, fieldsUsedCapacity).
		Build()
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
		Sells(vmpmAccessTemplateComm).   // sells to Pods
		Sells(numPodNumConsumersTemplateComm) // sells to Pods
		// also sells Cluster

	return nodeSupplyChainNodeBuilder.Create()
}

func (f *SupplyChainFactory) buildQuotaSupplyBuilder() (*proto.TemplateDTO, error) {
	nodeSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_VIRTUAL_DATACENTER)
	nodeSupplyChainNodeBuilder = nodeSupplyChainNodeBuilder.
		Sells(cpuAllocationTemplateCommWithKey).
		Sells(memAllocationTemplateCommWithKey).
		Sells(cpuRequestAllocationTemplateCommWithKey).
		Sells(memRequestAllocationTemplateCommWithKey)

	return nodeSupplyChainNodeBuilder.Create()
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
		Buys(numPodNumConsumersTemplateComm).
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
		Sells(respTimeTemplateComm).
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
		Buys(transactionTemplateComm).
		Buys(respTimeTemplateComm)
	return vAppSupplyChainNodeBuilder.Create()
}
