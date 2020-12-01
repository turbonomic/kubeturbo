package stitching

import (
	"fmt"
	"strings"
	"testing"
)

func TestNewStitchingManager(t *testing.T) {

	items := []StitchingPropertyType{UUID, IP, UUID, IP}

	for _, ptype := range items {
		m := NewStitchingManager(ptype)
		if m.GetStitchType() != ptype {
			t.Errorf("Stitching type wrong: %v Vs. %v", m.stitchType, ptype)
		}
	}
}

func TestStitchingManager_SetNodeUuidGetterByProvider(t *testing.T) {
	items := [][]string{
		{awsPrefix + "hello", "AWS"},
		{azurePrefix + "hello", "Azure"},
		{vspherePrefix + "hello", "Vsphere"},
		{"random1", "Default"},
	}

	m := NewStitchingManager(IP)

	for _, pair := range items {
		m.SetNodeUuidGetterByProvider(pair[0])
		name := m.uuidGetter.Name()
		if name != pair[1] {
			t.Errorf("Wrong uuidGetter: %v Vs. %v", name, pair[1])
		}
	}
}

func TestStitchingManager_StoreStitchingValue_AWS(t *testing.T) {
	tests := [][]string{
		{"aws:///us-west-2a/i-0be85bb9db1707470", "aws::us-west-2::VM::i-0be85bb9db1707470", "node1"},
		{"aws:///ca-west-2a/i-0be85bb9db1707470", "aws::ca-west-2::VM::i-0be85bb9db1707470", "node2"},
		{"aws:///ca-central-1a/i-0be85bb9db1707470", "aws::ca-central-1::VM::i-0be85bb9db1707470", "node3"},
	}

	m := NewStitchingManager(UUID)
	m.SetNodeUuidGetterByProvider(tests[0][0])

	for _, pair := range tests {
		node := mockAwsNode(pair[0])
		node.Name = pair[2]
		m.StoreStitchingValue(node)
	}

	for _, pair := range tests {
		nodeName := pair[2]
		nodeId := pair[1]
		result, err := m.GetStitchingValue(nodeName)
		if err != nil {
			t.Errorf("Failed to get node UUID: %v", err)
			continue
		}

		if strings.Compare(result, nodeId) != 0 {
			t.Errorf("Wrong node UUDID %v Vs. %v", result, nodeId)
		}
	}
}

func TestStitchingManager_StoreStitchingValue_Azure(t *testing.T) {
	tests := [][]string{
		{"D4DD3FE4-7A31-C74F-BBA7-3AE729EABA6E", "azure::VM::d4dd3fe4-7a31-c74f-bba7-3ae729eaba6e,azure::VM::e43fddd4-317a-4fc7-bba7-3ae729eaba6e", "node1"},
		{"D4DD3FE4-7A31-C74F-BBA7-3AE729EABC7E", "azure::VM::d4dd3fe4-7a31-c74f-bba7-3ae729eabc7e,azure::VM::e43fddd4-317a-4fc7-bba7-3ae729eabc7e", "node2"},
	}

	m := NewStitchingManager(UUID)
	m.SetNodeUuidGetterByProvider(azurePrefix)

	for _, pair := range tests {
		node := mockAzureNode(pair[0])
		node.Name = pair[2]
		m.StoreStitchingValue(node)
	}

	for _, pair := range tests {
		nodeName := pair[2]
		nodeId := pair[1]
		result, err := m.GetStitchingValue(nodeName)
		if err != nil {
			t.Errorf("Failed to get node UUID: %v", err)
			continue
		}

		if strings.Compare(result, nodeId) != 0 {
			t.Errorf("Wrong node UUDID %v Vs. %v", result, nodeId)
		}
	}
}

func TestStitchingManager_StoreStitchingValue_Default(t *testing.T) {
	tests := [][]string{
		{"4200979A-4EF9-E49B-6BD6-FDBAD2BE7252", "4200979a-4ef9-e49b-6bd6-fdbad2be7252,9a970042-f94e-9be4-6bd6-fdbad2be7252", "node1"},
		{"D4DD3FE4-7A31-C74F-BBA7-3AE729EABC7E", "d4dd3fe4-7a31-c74f-bba7-3ae729eabc7e,e43fddd4-317a-4fc7-bba7-3ae729eabc7e", "node2"},
	}

	m := NewStitchingManager(UUID)
	m.SetNodeUuidGetterByProvider("")

	for _, pair := range tests {
		node := mockNode(pair[0])
		node.Name = pair[2]
		m.StoreStitchingValue(node)
	}

	for _, pair := range tests {
		nodeName := pair[2]
		nodeId := pair[1]
		result, err := m.GetStitchingValue(nodeName)
		if err != nil {
			t.Errorf("Failed to get node UUID: %v", err)
			continue
		}

		if strings.Compare(result, nodeId) != 0 {
			t.Errorf("Wrong node UUDID %v Vs. %v", result, nodeId)
		}
	}
}

func TestStitchingManager_BuildDTOLayerOverProperty(t *testing.T) {
	tests := [][]string{
		{"D4DD3FE4-7A31-C74F-BBA7-3AE729EABA6E", "azure::VM::e43fddd4-317a-4fc7-bba7-3ae729eaba6e", "node1"},
		{"D4DD3FE4-7A31-C74F-BBA7-3AE729EABC7E", "azure::VM::e43fddd4-317a-4fc7-bba7-3ae729eabc7e", "node2"},
	}

	m := NewStitchingManager(UUID)
	m.SetNodeUuidGetterByProvider(azurePrefix)

	nodeNames := []string{}
	for _, pair := range tests {
		node := mockAzureNode(pair[0])
		node.Name = pair[2]
		m.StoreStitchingValue(node)
		nodeNames = append(nodeNames, node.Name)
	}

	//m.stitchType = IP
	property, err := m.BuildDTOLayerOverProperty(nodeNames)
	if err != nil {
		t.Errorf("Failed to build LayerOver property: %v", err)
	} else {
		fmt.Printf("%++v\n", property)
	}
}

func TestStitchingManager_BuildDTOLayerOverProperty_Default(t *testing.T) {
	tests := [][]string{
		{"4200979A-4EF9-E49B-6BD6-FDBAD2BE7252", "4200979a-4ef9-e49b-6bd6-fdbad2be7252", "node1"},
		{"D4DD3FE4-7A31-C74F-BBA7-3AE729EABC7E", "d4dd3fe4-7a31-c74f-bba7-3ae729eabc7e", "node2"},
	}

	m := NewStitchingManager(UUID)
	m.SetNodeUuidGetterByProvider("")

	nodeNames := []string{}
	for _, pair := range tests {
		node := mockAzureNode(pair[0])
		node.Name = pair[2]
		m.StoreStitchingValue(node)
		nodeNames = append(nodeNames, node.Name)
	}

	//m.stitchType = IP
	property, err := m.BuildDTOLayerOverProperty(nodeNames)
	if err != nil {
		t.Errorf("Failed to build LayerOver property: %v", err)
	} else {
		fmt.Printf("%++v\n", property)
	}
}
