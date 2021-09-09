package stitching

import (
	"strings"
	"testing"

	api "k8s.io/api/core/v1"
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

func mockGKENode(uuid string) *api.Node {
	node := &api.Node{}

	node.Spec.ProviderID = uuid
	addAnnotation(node, "container.googleapis.com/instance_id", "8108478110475488564")
	return node
}

func addAnnotation(node *api.Node, key, value string) {
	annotations := node.ObjectMeta.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
		node.Annotations = annotations
	}
	annotations[key] = value
	return
}

func mockVsphereNode(uuid, systemUUID string) *api.Node {
	node := &api.Node{}

	if uuid != "" {
		node.Spec.ProviderID = vspherePrefix + uuid
	}
	if systemUUID != "" {
		node.Status.NodeInfo.SystemUUID = systemUUID
	}
	return node
}

func TestDefaultNodeUUIDGetter_GetUUID(t *testing.T) {
	tests := [][]string{
		{"4200979A-4EF9-E49B-6BD6-FDBAD2BE7252", "4200979a-4ef9-e49b-6bd6-fdbad2be7252,9a970042-f94e-9be4-6bd6-fdbad2be7252"},
		{"DE7D3FE4-7A31-C74F-BBA7-3AE729EABC7E", "de7d3fe4-7a31-c74f-bba7-3ae729eabc7e,e43f7dde-317a-4fc7-bba7-3ae729eabc7e"},
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
		{"D4DD3FE4-7A31-C74F-BBA7-3AE729EABA6E", "azure::VM::d4dd3fe4-7a31-c74f-bba7-3ae729eaba6e,azure::VM::e43fddd4-317a-4fc7-bba7-3ae729eaba6e"},
		{"D4DD3FE4-7A31-C74F-BBA7-3AE729EABC7E", "azure::VM::d4dd3fe4-7a31-c74f-bba7-3ae729eabc7e,azure::VM::e43fddd4-317a-4fc7-bba7-3ae729eabc7e"},
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

func TestVsphereNodeUUIDGetter_GetUUID(t *testing.T) {
	tests := [][]string{
		{"D4DD3FE4-7A31-C74F-BBA7-3AE729EABA6E", "d4dd3fe4-7a31-c74f-bba7-3ae729eaba6e,e43fddd4-317a-4fc7-bba7-3ae729eaba6e"},
		{"D4DD3FE4-7A31-C74F-BBA7-3AE729EABA6E", "D4DD3FE4-7A31-C74F-BBA7-3AE729EABA6E",
			"d4dd3fe4-7a31-c74f-bba7-3ae729eaba6e,e43fddd4-317a-4fc7-bba7-3ae729eaba6e"},
		{"D4DD3FE4-7A31-C74F-BBA7-3AE729EABA6E", "D4DD3FE4-7A31-C74F-BBA7-3AE729EABC7E",
			"d4dd3fe4-7a31-c74f-bba7-3ae729eaba6e,e43fddd4-317a-4fc7-bba7-3ae729eaba6e,d4dd3fe4-7a31-c74f-bba7-3ae729eabc7e,e43fddd4-317a-4fc7-bba7-3ae729eabc7e"},
	}

	vsphere := &vsphereNodeUUIDGetter{}

	for _, strs := range tests {
		providerID, systemUUID, allIDs := "", "", ""
		providerID = strs[0]
		if len(strs) == 3 {
			systemUUID = strs[1]
			allIDs = strs[2]
		} else {
			allIDs = strs[1]
		}

		node := mockVsphereNode(providerID, systemUUID)
		result, err := vsphere.GetUUID(node)

		if err != nil {
			t.Errorf("Failed to get Vsphere node UUID: %v", err)
			continue
		}
		if strings.Compare(result, allIDs) != 0 {
			t.Errorf("Wrong node UUID %v Vs. %v", result, allIDs)
		}
	}
}

func TestGKENodeUUIDGetter_GetUUID(t *testing.T) {
	tests := [][]string{
		{"gce://turbonomic-eng/us-central1-a/gke-enlin-cluster-1-default-pool-b0f2516c-mrl0", "gcp::us-central1-a::VM::8108478110475488564"},
	}

	gke := &gceNodeUUIDGetter{}

	for _, pair := range tests {
		node := mockGKENode(pair[0])
		result, err := gke.GetUUID(node)

		if err != nil {
			t.Errorf("Failed to get GKE node UUID: %v", err)
			continue
		}

		if strings.Compare(result, pair[1]) != 0 {
			t.Errorf("Wrong node UUID %v Vs. %v", result, pair[1])
		}
	}
}
