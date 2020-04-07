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
	vCpuType               = proto.CommodityDTO_VCPU
	vMemType               = proto.CommodityDTO_VMEM
	vCpuRequestType        = proto.CommodityDTO_VCPU_REQUEST
	vMemRequestType        = proto.CommodityDTO_VMEM_REQUEST
	vCpuLimitQuotaType     = proto.CommodityDTO_VCPU_LIMIT_QUOTA
	vMemLimitQuotaType     = proto.CommodityDTO_VMEM_LIMIT_QUOTA
	vCpuRequestQuotaType   = proto.CommodityDTO_VCPU_REQUEST_QUOTA
	vMemRequestQuotaType   = proto.CommodityDTO_VMEM_REQUEST_QUOTA
	clusterType            = proto.CommodityDTO_CLUSTER
	vmPMAccessType         = proto.CommodityDTO_VMPM_ACCESS
	appCommType            = proto.CommodityDTO_APPLICATION
	numPodNumConsumersType = proto.CommodityDTO_NUMBER_CONSUMERS
	vStorageType           = proto.CommodityDTO_VSTORAGE

	fakeKey = "fake"

	vCpuTemplateComm                   = &proto.TemplateCommodity{CommodityType: &vCpuType}
	vMemTemplateComm                   = &proto.TemplateCommodity{CommodityType: &vMemType}
	vCpuRequestTemplateComm            = &proto.TemplateCommodity{CommodityType: &vCpuRequestType}
	vMemRequestTemplateComm            = &proto.TemplateCommodity{CommodityType: &vMemRequestType}
	numPodNumConsumersTemplateComm     = &proto.TemplateCommodity{CommodityType: &numPodNumConsumersType}
	vStorageTemplateComm               = &proto.TemplateCommodity{CommodityType: &vStorageType}
	cpuLimitQuotaTemplateCommWithKey   = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &vCpuLimitQuotaType}
	memLimitQuotaTemplateCommWithKey   = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &vMemLimitQuotaType}
	cpuRequestQuotaTemplateCommWithKey = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &vCpuRequestQuotaType}
	memRequestQuotaTemplateCommWithKey = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &vMemRequestQuotaType}
	vmpmAccessTemplateComm             = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &vmPMAccessType}
	applicationTemplateCommWithKey     = &proto.TemplateCommodity{Key: &fakeKey, CommodityType: &appCommType}

	// Internal matching property
	proxyVMIP   = "Proxy_VM_IP"
	proxyVMUUID = "Proxy_VM_UUID"

	// External matching property
	VMIPFieldName          = supplychain.SUPPLY_CHAIN_CONSTANT_IP_ADDRESS
	VMIPFieldPaths         = []string{supplychain.SUPPLY_CHAIN_CONSTANT_VIRTUAL_MACHINE_DATA}
	VMUUID                 = supplychain.SUPPLY_CHAIN_CONSTANT_ID
	ActionEligibilityField = "actionEligibility"
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
	namespaceSupplyChainNode, err := f.buildNamespaceSupplyBuilder()
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("Supply chain node: %+v", namespaceSupplyChainNode)

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

	// ContainerSpec supply chain template
	containerSpecSupplyChainNode, err := f.buildContainerSpecSupplyBuilder()
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("Supply chain node: %+v", containerSpecSupplyChainNode)

	supplyChainBuilder := supplychain.NewSupplyChainBuilder()
	supplyChainBuilder.Top(vAppSupplyChainNode)
	supplyChainBuilder.Entity(appSupplyChainNode)
	supplyChainBuilder.Entity(containerSupplyChainNode)
	supplyChainBuilder.Entity(containerSpecSupplyChainNode)
	supplyChainBuilder.Entity(podSupplyChainNode)
	supplyChainBuilder.Entity(namespaceSupplyChainNode)
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
		builder.PropertyUsed:      {},
		builder.PropertyCapacity:  {},
		builder.PropertyPeak:      {},
		builder.PropertyResizable: {},
	}
	mergedEntityMetadataBuilder := builder.NewMergedEntityMetadataBuilder()

	mergedEntityMetadataBuilder.PatchField(ActionEligibilityField, []string{})
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
		PatchSoldMetadata(proto.CommodityDTO_VCPU_LIMIT_QUOTA, fieldsUsedCapacity).
		PatchSoldMetadata(proto.CommodityDTO_VMEM_LIMIT_QUOTA, fieldsUsedCapacity).
		PatchSoldMetadata(proto.CommodityDTO_VCPU_REQUEST_QUOTA, fieldsUsedCapacity).
		PatchSoldMetadata(proto.CommodityDTO_VMEM_REQUEST_QUOTA, fieldsUsedCapacity).
		PatchSoldMetadata(proto.CommodityDTO_NUMBER_CONSUMERS, fieldsUsedCapacity).
		PatchSoldMetadata(proto.CommodityDTO_VSTORAGE, fieldsUsedCapacity).
		Build()
}

func (f *SupplyChainFactory) buildNodeSupplyBuilder() (*proto.TemplateDTO, error) {
	nodeSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_VIRTUAL_MACHINE)
	nodeSupplyChainNodeBuilder.SetPriority(f.vmPriority)
	nodeSupplyChainNodeBuilder.SetTemplateType(f.vmTemplateType)

	nodeSupplyChainNodeBuilder = nodeSupplyChainNodeBuilder.
		Sells(vCpuTemplateComm).               // sells to Pods
		Sells(vMemTemplateComm).               // sells to Pods
		Sells(vCpuRequestTemplateComm).        // sells to Pods
		Sells(vMemRequestTemplateComm).        // sells to Pods
		Sells(vmpmAccessTemplateComm).         // sells to Pods
		Sells(numPodNumConsumersTemplateComm). // sells to Pods
		Sells(vStorageTemplateComm)            // sells to Pods
		// also sells Cluster to Pods

	return nodeSupplyChainNodeBuilder.Create()
}

func (f *SupplyChainFactory) buildNamespaceSupplyBuilder() (*proto.TemplateDTO, error) {
	namespaceSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_NAMESPACE)
	namespaceSupplyChainNodeBuilder = namespaceSupplyChainNodeBuilder.
		Sells(cpuLimitQuotaTemplateCommWithKey).
		Sells(memLimitQuotaTemplateCommWithKey).
		Sells(cpuRequestQuotaTemplateCommWithKey).
		Sells(memRequestQuotaTemplateCommWithKey)
	return namespaceSupplyChainNodeBuilder.Create()
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
		Buys(vStorageTemplateComm).
		Provider(proto.EntityDTO_NAMESPACE, proto.Provider_HOSTING).
		Buys(cpuLimitQuotaTemplateCommWithKey).
		Buys(memLimitQuotaTemplateCommWithKey).
		Buys(cpuRequestQuotaTemplateCommWithKey).
		Buys(memRequestQuotaTemplateCommWithKey)

	// Link from Pod to VM
	vmPodExtLinkBuilder := supplychain.NewExternalEntityLinkBuilder()
	vmPodExtLinkBuilder.Link(proto.EntityDTO_CONTAINER_POD, proto.EntityDTO_VIRTUAL_MACHINE, proto.Provider_HOSTING).
		Commodity(vCpuType, false).
		Commodity(vMemType, false).
		Commodity(vCpuRequestType, false).
		Commodity(vMemRequestType, false).
		Commodity(numPodNumConsumersType, false).
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

func (f *SupplyChainFactory) buildContainerSpecSupplyBuilder() (*proto.TemplateDTO, error) {
	containerSpecSupplyChainNodeBuilder := supplychain.NewSupplyChainNodeBuilder(proto.EntityDTO_CONTAINER_SPEC)
	// TODO set up commodities sold by ContainerSpec here
	return containerSpecSupplyChainNodeBuilder.Create()
}
