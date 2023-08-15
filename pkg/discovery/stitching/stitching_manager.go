package stitching

import (
	"fmt"
	"strings"

	api "k8s.io/api/core/v1"

	"github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.com/turbonomic/turbo-go-sdk/pkg/supplychain"

	"github.com/golang/glog"
)

const (
	UUID StitchingPropertyType = "UUID"
	IP   StitchingPropertyType = "IP"

	// The property used for node property and replacement entity metadata
	proxyVMIP       string = "Proxy_VM_IP"
	proxyVMUUID     string = "Proxy_VM_UUID"
	PodID           string = "POD"
	ContainerID     string = "CNT"
	ContainerFullID string = "CNTFULL"
	ContainerIDlen  int    = 12

	// The default namespace of entity property
	DefaultPropertyNamespace string = "DEFAULT"

	// The attribute used for stitching with other probes (e.g., prometurbo) with app and service
	AppStitchingAttr string = "IP"

	// The attributes used for stitching kubeturbo discovered services with services discovered by the Dynatrace probes.
	ServiceNameStitchingAttr       string = "SERVICE_NAME"
	ServiceNamespaceStitchingAttr  string = "SERVICE_NAMESPACE"
	KubeSystemUIDStitchingAttr     string = "KUBE_SYSTEM_UID"
	DynaTraceServiceStitichingAttr string = "DYNATRACE_SERVICE_ID"
)

// The property type that is used for stitching. For example "UUID", "IP address".
type StitchingPropertyType string

type StitchingManager struct {
	// key: node name; value: UID or IP for stitching
	nodeStitchingIDMap map[string]string

	// The property used for stitching.
	stitchType StitchingPropertyType

	// get node reconcile UUID
	uuidGetter NodeUUIDGetter
}

func NewStitchingManager(pType StitchingPropertyType) *StitchingManager {
	if pType != UUID && pType != IP {
		glog.Errorf("Wrong stitching type: %v, only [%v, %v] are acceptable", pType, UUID, IP)
	}

	return &StitchingManager{
		stitchType:         pType,
		uuidGetter:         &defaultNodeUUIDGetter{},
		nodeStitchingIDMap: make(map[string]string),
	}
}

func (s *StitchingManager) SetNodeUuidGetterByProvider(providerId string) {
	if s.stitchType == IP {
		glog.Warningf("Stitching type is IP, no need to set NodeUuidGetter")
	}

	var getter NodeUUIDGetter

	getter = &defaultNodeUUIDGetter{}

	if strings.HasPrefix(providerId, vspherePrefix) {
		getter = &vsphereNodeUUIDGetter{}
	} else if strings.HasPrefix(providerId, awsPrefix) {
		getter = &awsNodeUUIDGetter{}
	} else if strings.HasPrefix(providerId, azurePrefix) {
		getter = &azureNodeUUIDGetter{}
	} else if strings.HasPrefix(providerId, gcePrefix) {
		getter = &gceNodeUUIDGetter{}
	}

	s.uuidGetter = getter
	glog.V(4).Infof("Node UUID getter is: %v", getter.Name())
}

func (s *StitchingManager) GetStitchType() StitchingPropertyType {
	return s.stitchType
}

// Retrieve stitching values from given node and store in maps.
// Do nothing if it is a local testing.
func (s *StitchingManager) StoreStitchingValue(node *api.Node) {
	if node == nil {
		glog.Error("input node is nil")
		return
	}

	if s.stitchType == UUID {
		s.storeNodeUUID(node)
	} else {
		s.storeNodeIP(node)
	}
}

// Find the IP address of the node and store it in nodeStitchingIPMap.
func (s *StitchingManager) storeNodeIP(node *api.Node) {
	nodeStitchingIP := getStitchingIP(node)

	if nodeStitchingIP == "" {
		glog.Errorf("Failed to find stitching IP for node %v: no external nor interal IP ", node.Name)
		return
	}

	s.nodeStitchingIDMap[node.Name] = nodeStitchingIP
}

// Get the systemUUID of the node and store it in nodeStitchingUIDMap.
func (s *StitchingManager) storeNodeUUID(node *api.Node) {
	nodeStitchingID, err := s.uuidGetter.GetUUID(node)
	if err != nil || nodeStitchingID == "" {
		glog.Errorf("Failed to get stitching UUID for node %v: %v", node.Name, err)
		return
	}

	s.nodeStitchingIDMap[node.Name] = nodeStitchingID
}

// Get the stitching value based on given nodeName.
// Return localTestStitchingValue if it is a local testing.
func (s *StitchingManager) GetStitchingValue(nodeName string) (string, error) {
	sid, exist := s.nodeStitchingIDMap[nodeName]
	if !exist {
		err := fmt.Errorf("Failed to get stitching value for node %v, type=%v", nodeName, s.stitchType)
		glog.Error(err.Error())
		return "", err
	}

	return sid, nil
}

// Build the stitching node property for entity based on the given node name, and purpose.
//
//	two purposes: "stitching" and "reconcile".
//	    stitching: is to stitch Pod to the real-VM;
//	    reconcile: is to merge the proxy-VM to the real-VM;
func (s *StitchingManager) BuildDTOProperty(nodeName string, isForReconcile bool) (*proto.EntityDTO_EntityProperty, error) {
	propertyNamespace := DefaultPropertyNamespace
	propertyName := s.getPropertyName(isForReconcile)
	propertyValue, err := s.GetStitchingValue(nodeName)
	if err != nil {
		return nil, fmt.Errorf("Failed to build entity stitching property: %s", err)
	}
	return &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &propertyName,
		Value:     &propertyValue,
	}, nil
}

// Stitch one entity with a list of VMs.
func (s *StitchingManager) BuildDTOLayerOverProperty(nodeNames []string) (*proto.EntityDTO_EntityProperty, error) {
	propertyNamespace := DefaultPropertyNamespace
	propertyName := s.getStitchingPropertyName()

	values := []string{}
	for _, nodeName := range nodeNames {
		value, err := s.GetStitchingValue(nodeName)
		if err != nil {
			glog.Errorf("Failed to build DTO stitching property: %v", err)
			return nil, err
		}
		values = append(values, value)
	}
	propertyValue := strings.Join(values, ",")

	return &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &propertyName,
		Value:     &propertyValue,
	}, nil
}

// Get the property name based on whether it is a stitching or reconciliation.
func (s *StitchingManager) getPropertyName(isForReconcile bool) string {
	if isForReconcile {
		return s.getReconciliationPropertyName()
	}

	return s.getStitchingPropertyName()
}

// Get the name of property for entities reconciliation.
func (s *StitchingManager) getReconciliationPropertyName() string {
	if s.stitchType == UUID {
		return proxyVMUUID
	}

	return proxyVMIP
}

// Get the name of property for entities stitching.
func (s *StitchingManager) getStitchingPropertyName() string {
	if s.stitchType == UUID {
		return supplychain.SUPPLY_CHAIN_CONSTANT_UUID
	}
	return supplychain.SUPPLY_CHAIN_CONSTANT_IP_ADDRESS
}

// Create the meta data that will be used during the reconciliation process.
// This seems only applicable for VirtualMachines.
func (s *StitchingManager) GenerateReconciliationMetaData() (*proto.EntityDTO_ReplacementEntityMetaData, error) {
	replacementEntityMetaDataBuilder := builder.NewReplacementEntityMetaDataBuilder()
	switch s.stitchType {
	case UUID:
		replacementEntityMetaDataBuilder.Matching(proxyVMUUID).MatchingExternal(supplychain.VM_UUID)
	case IP:
		replacementEntityMetaDataBuilder.Matching(proxyVMIP).MatchingExternal(supplychain.VM_IP)
	default:
		return nil, fmt.Errorf("stitching property type %s is not supported", s.stitchType)
	}
	usedAndCapacityPropertyNames := []string{builder.PropertyCapacity, builder.PropertyUsed}
	vcpuUsedAndCapacityPropertyNames := []string{builder.PropertyCapacity, builder.PropertyUsed, builder.PropertyPeak}
	capacityOnlyPropertyNames := []string{builder.PropertyCapacity}
	replacementEntityMetaDataBuilder.PatchSellingWithProperty(proto.CommodityDTO_CLUSTER, capacityOnlyPropertyNames).
		PatchSellingWithProperty(proto.CommodityDTO_VMPM_ACCESS, capacityOnlyPropertyNames).
		PatchSellingWithProperty(proto.CommodityDTO_VCPU, vcpuUsedAndCapacityPropertyNames).
		PatchSellingWithProperty(proto.CommodityDTO_VMEM, usedAndCapacityPropertyNames).
		PatchSellingWithProperty(proto.CommodityDTO_VCPU_REQUEST, usedAndCapacityPropertyNames).
		PatchSellingWithProperty(proto.CommodityDTO_VMEM_REQUEST, usedAndCapacityPropertyNames).
		PatchSellingWithProperty(proto.CommodityDTO_VCPU_LIMIT_QUOTA, usedAndCapacityPropertyNames).
		PatchSellingWithProperty(proto.CommodityDTO_VMEM_LIMIT_QUOTA, usedAndCapacityPropertyNames).
		PatchSellingWithProperty(proto.CommodityDTO_VCPU_REQUEST_QUOTA, usedAndCapacityPropertyNames).
		PatchSellingWithProperty(proto.CommodityDTO_VMEM_REQUEST_QUOTA, usedAndCapacityPropertyNames).
		PatchSellingWithProperty(proto.CommodityDTO_NUMBER_CONSUMERS, usedAndCapacityPropertyNames).
		PatchSellingWithProperty(proto.CommodityDTO_VSTORAGE, usedAndCapacityPropertyNames)
	meta := replacementEntityMetaDataBuilder.Build()
	return meta, nil
}

// Use external IP if it is available. Otherwise use legacy host IP
func getStitchingIP(node *api.Node) string {
	// Node IP Address
	ip := parseNodeIP(node, api.NodeExternalIP)
	if ip == "" {
		ip = parseNodeIP(node, api.NodeInternalIP)
	}
	if ip == "" {
		glog.Errorf("Failed to find IP for node %s", node.Name)
	}

	return ip
}

// Parse the Node instances returned by the kubernetes API to get the IP address
func parseNodeIP(node *api.Node, addressType api.NodeAddressType) string {
	nodeAddresses := node.Status.Addresses
	for _, nodeAddress := range nodeAddresses {
		if nodeAddress.Type == addressType && nodeAddress.Address != "" {
			glog.V(4).Infof("%s : %s is %s", node.Name, addressType, nodeAddress.Address)
			return nodeAddress.Address
		}
	}
	return ""
}
