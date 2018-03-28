package stitching

import (
	api "k8s.io/client-go/pkg/api/v1"
	"strings"
	"testing"
)

func mockNode(uuid string) *api.Node {
	node := &api.Node{}
	node.Status.NodeInfo.SystemUUID = uuid
	return node
}

func mockAwsNode(providerId string) *api.Node {
	node := &api.Node{}
	node.Spec.ProviderID = providerId

	return node
}

func mockAzureNode(uuid string) *api.Node {
	node := &api.Node{}

	node.Spec.ProviderID = azurePrefix + "subscrition"
	node.Status.NodeInfo.SystemUUID = uuid
	return node
}

func TestDefaultNodeUUIDGetter_GetUUID(t *testing.T) {
	tests := [][]string{
		{"4200979A-4EF9-E49B-6BD6-FDBAD2BE7252", "4200979a-4ef9-e49b-6bd6-fdbad2be7252"},
		{"D4DD3FE4-7A31-C74F-BBA7-3AE729EABC7E", "d4dd3fe4-7a31-c74f-bba7-3ae729eabc7e"},
		{"DE7D3FE4-7A31-C74F-BBA7-3AE729EABC7E", "de7d3fe4-7a31-c74f-bba7-3ae729eabc7e"},
	}

	vm := &defaultNodeUUIDGetter{}

	for _, pair := range tests {
		node := mockNode(pair[0])
		result, err := vm.GetUUID(node)

		if err != nil {
			t.Errorf("Failed to get Azure node UUID: %v", err)
			continue
		}

		if strings.Compare(result, pair[1]) != 0 {
			t.Errorf("Wrong node UUID %v Vs. %v", result, pair[1])
		}
	}
}

func TestAWSNodeUUIDGetter_GetUUID(t *testing.T) {
	tests := [][]string{
		{"aws:///us-west-2a/i-0be85bb9db1707470", "aws::us-west-2::VM::i-0be85bb9db1707470"},
		{"aws:///ca-west-2a/i-0be85bb9db1707470", "aws::ca-west-2::VM::i-0be85bb9db1707470"},
		{"aws:///ca-central-1a/i-0be85bb9db1707470", "aws::ca-central-1::VM::i-0be85bb9db1707470"},
	}

	aws := &awsNodeUUIDGetter{}

	for _, pair := range tests {
		node := mockAwsNode(pair[0])
		result, err := aws.GetUUID(node)

		if err != nil {
			t.Errorf("Failed to get node UUID: %v", err)
			continue
		}

		if strings.Compare(result, pair[1]) != 0 {
			t.Errorf("Wrong node UUDID %v Vs. %v", result, pair[1])
		}
	}
}

func TestAzureNodeUUIDGetter_GetUUID(t *testing.T) {
	tests := [][]string{
		{"D4DD3FE4-7A31-C74F-BBA7-3AE729EABA6E", "azure::VM::e43fddd4-317a-4fc7-bba7-3ae729eaba6e"},
		{"D4DD3FE4-7A31-C74F-BBA7-3AE729EABC7E", "azure::VM::e43fddd4-317a-4fc7-bba7-3ae729eabc7e"},
		{"DE7D3FE4-7A31-C74F-BBA7-3AE729EABC7E", "azure::VM::e43f7dde-317a-4fc7-bba7-3ae729eabc7e"},
	}

	azure := &azureNodeUUIDGetter{}

	for _, pair := range tests {
		node := mockAzureNode(pair[0])
		result, err := azure.GetUUID(node)

		if err != nil {
			t.Errorf("Failed to get Azure node UUID: %v", err)
			continue
		}

		if strings.Compare(result, pair[1]) != 0 {
			t.Errorf("Wrong node UUID %v Vs. %v", result, pair[1])
		}
	}
}
