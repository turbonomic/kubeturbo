package stitching

import (
	"errors"
	"fmt"
	"strings"

	"k8s.io/kubernetes/pkg/api"

	"github.com/turbonomic/kubeturbo/test/flag"

	"github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.com/turbonomic/turbo-go-sdk/pkg/supplychain"

	"github.com/golang/glog"
)

const (
	// The property used for node property and replacement entity metadata
	proxyVMIP   string = "Proxy_VM_IP"
	proxyVMUUID string = "Proxy_VM_UUID"

	// The default namespace of entity property
	defaultPropertyNamespace string = "DEFAULT"

	Stitch    stitchingPropertyType = "stitch"
	Reconcile stitchingPropertyType = "reconcile"
)

type stitchingPropertyType string

type StitchingManager struct {
	// key: node name; value: node IP address for stitching.
	nodeStitchingUIDMap map[string]string

	// key: node name; value: node IP address for stitching.
	nodeStitchingIPMap map[string]string

	//
	useVMWare bool

	localTestingFlags *flag.TestingFlag
}

func NewStitchingManager() *StitchingManager {
	testingFlag := flag.GetFlag()
	return &StitchingManager{
		localTestingFlags: testingFlag,
	}
}

func (s *StitchingManager) UseVMWare(u bool) *StitchingManager {
	s.useVMWare = u
	return s
}

// Retrieve stitching values from given node and store in maps.
// Do nothing if it is a local testing.
func (s *StitchingManager) StoreStitchingValue(node *api.Node) {
	if s.localTestingFlags.LocalTestingFlag {
		return
	}
	if s.useVMWare {
		s.retrieveAndStoreStitchingUUID(node)
	} else {
		s.retrieveAndStoreStitchingIP(node)
	}
}

// Find the IP address of the node and store it in nodeStitchingIPMap.
func (s *StitchingManager) retrieveAndStoreStitchingIP(node *api.Node) {
	var nodeStitchingIP string
	nodeAddresses := node.Status.Addresses
	// Use external IP if it is available. Otherwise use legacy host IP.
	for _, nodeAddress := range nodeAddresses {
		if nodeAddress.Type == api.NodeExternalIP {
			nodeStitchingIP = nodeAddress.Address
		}
		if nodeStitchingIP == "" && nodeAddress.Type == api.NodeLegacyHostIP {
			nodeStitchingIP = nodeAddress.Address
		}
	}

	if nodeStitchingIP == "" {
		glog.Errorf("Failed to find stitching IP for node %s: it does not have either external IP or legacy "+
			"host IP.", node.Name)
	} else {
		if s.nodeStitchingIPMap == nil {
			s.nodeStitchingIPMap = make(map[string]string)
		}
		s.nodeStitchingIPMap[node.Name] = nodeStitchingIP
	}
}

// Get the systemUUID of the node and store it in nodeStitchingUIDMap.
func (s *StitchingManager) retrieveAndStoreStitchingUUID(node *api.Node) {
	nodeStitchingID := node.Status.NodeInfo.SystemUUID
	if nodeStitchingID == "" {
		glog.Errorf("Invalid stitching UUID for node %s", node.Name)
	} else {
		if s.nodeStitchingUIDMap == nil {
			s.nodeStitchingUIDMap = make(map[string]string)
		}
		s.nodeStitchingUIDMap[node.Name] = strings.ToLower(nodeStitchingID)
	}
}

// Get the stitching value based on given nodeName.
// Return localTestStitchingValue if it is a local testing.
func (s *StitchingManager) GetStitchingValue(nodeName string) (string, error) {
	if s.localTestingFlags.LocalTestingFlag {
		if s.localTestingFlags.LocalTestStitchingValue == "" {
			return "", errors.New("Local testing stitching value is empty.")
		} else {
			return s.localTestingFlags.LocalTestStitchingValue, nil
		}
	} else {
		if s.useVMWare {
			return s.getNodeUUIDForStitching(nodeName)
		} else {
			return s.getNodeIPForStitching(nodeName)
		}
	}
}

// Get the correct IP that will be used during the stitching process.
func (s *StitchingManager) getNodeIPForStitching(nodeName string) (string, error) {
	if s.nodeStitchingIPMap == nil {
		return "", errors.New("No stitching IP available.")
	}
	nodeIP, exist := s.nodeStitchingIPMap[nodeName]
	if !exist {
		return "", fmt.Errorf("Failed to get stitching IP of node %s", nodeName)
	}

	return nodeIP, nil
}

// Find the system UUID that will be used during the stitching process.
func (s *StitchingManager) getNodeUUIDForStitching(nodeName string) (string, error) {
	if s.nodeStitchingUIDMap == nil {
		return "", errors.New("No stitching UUID available.")
	}
	nodeUUID, exist := s.nodeStitchingUIDMap[nodeName]
	if !exist {
		return "", fmt.Errorf("Failed to get stitching UUID of node %s", nodeName)
	}

	return nodeUUID, nil
}

// Build the stitching node property for entity based on the given node name and stitching property type.
func (s *StitchingManager) BuildStitchingProperty(nodeName string, pType stitchingPropertyType) (*proto.EntityDTO_EntityProperty, error) {
	propertyNamespace := defaultPropertyNamespace
	propertyName, err := s.getStitchingPropertyName(pType)
	if err != nil {
		return nil, fmt.Errorf("Failed to build entity stitching property: %s", err)
	}
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

func (s *StitchingManager) getStitchingPropertyName(pType stitchingPropertyType) (string, error) {
	switch pType {
	case Reconcile:
		if s.useVMWare {
			return proxyVMUUID, nil
		} else {
			return proxyVMIP, nil
		}
	case Stitch:
		if s.useVMWare {
			return supplychain.SUPPLY_CHAIN_CONSTANT_UUID, nil
		} else {
			return supplychain.SUPPLY_CHAIN_CONSTANT_IP_ADDRESS, nil
		}
	}
	return "", fmt.Errorf("Stitching property type %s is not supported.", pType)
}

// Create the meta data that will be used during the reconciliation process.
func (s *StitchingManager) GenerateReconciliationMetaData() *proto.EntityDTO_ReplacementEntityMetaData {
	replacementEntityMetaDataBuilder := builder.NewReplacementEntityMetaDataBuilder()
	if s.useVMWare {
		replacementEntityMetaDataBuilder.Matching(proxyVMUUID)
	} else {
		replacementEntityMetaDataBuilder.Matching(proxyVMIP)
	}
	replacementEntityMetaDataBuilder.PatchSelling(proto.CommodityDTO_CLUSTER).
		PatchSelling(proto.CommodityDTO_APPLICATION).
		PatchSelling(proto.CommodityDTO_VMPM_ACCESS)
	return replacementEntityMetaDataBuilder.Build()
}
