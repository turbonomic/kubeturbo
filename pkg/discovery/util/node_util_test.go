package util

import (
	"testing"

	set "github.com/deckarep/golang-set"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodeMatchesLabels(t *testing.T) {
	// Custom labels with unique keys
	customLabels1, _ := LabelMapFromNodeSelectorString("key1=value1,key2=value2")
	// Custom labels with same key but different values
	customLabels2, _ := LabelMapFromNodeSelectorString("kubernetes.io/arch=s390x,kubernetes.io/arch=arm64")

	windowsLabels1 := map[string]string{
		"kubernetes.io/os":      "windows",
		"beta.kubernetes.io/os": "windows",
		"node1":                 "value1",
	}

	windowsLabels2 := map[string]string{
		"beta.kubernetes.io/os": "windows",
		"node1":                 "value1",
		"key1":                  "value1",
	}

	windowsLabels3 := map[string]string{
		"beta.kubernetes.io/os": "windows",
		"key1":                  "value1",
		"key2":                  "value2",
	}

	linuxLabels := map[string]string{
		"kubernetes.io/os": "linux",
		"node2":            "value2",
	}

	s390xLabels := map[string]string{
		"kubernetes.io/arch": "s390x",
		"node2":              "value2",
	}

	arm64Labels := map[string]string{
		"kubernetes.io/arch": "arm64",
		"node3":              "value3",
	}

	type labelTest struct {
		mustMatchLabels map[string]set.Set
		node            *v1.Node
		expectMatch     bool
	}

	tests := []labelTest{
		{
			mustMatchLabels: nil,
			node:            getNodeWithLabels(windowsLabels1),
			expectMatch:     false,
		},
		{
			mustMatchLabels: nil,
			node:            getNodeWithLabels(windowsLabels2),
			expectMatch:     false,
		},
		{
			mustMatchLabels: nil,
			node:            getNodeWithLabels(linuxLabels),
			expectMatch:     false,
		},
		{
			mustMatchLabels: customLabels1,
			node:            getNodeWithLabels(windowsLabels3),
			expectMatch:     true,
		},
		{
			mustMatchLabels: customLabels1,
			node:            getNodeWithLabels(windowsLabels2),
			expectMatch:     false,
		},
		{
			mustMatchLabels: customLabels2,
			node:            getNodeWithLabels(s390xLabels),
			expectMatch:     true,
		},
		{
			mustMatchLabels: customLabels2,
			node:            getNodeWithLabels(arm64Labels),
			expectMatch:     true,
		},
	}

	for _, test := range tests {
		match := NodeMatchesLabels(test.node, test.mustMatchLabels)
		if match != test.expectMatch {
			t.Errorf("Label match failed, Nodes labels: %+v, mustMatchLabels: %+v."+
				"Expected match: %v, got: %v", test.node.Labels, test.mustMatchLabels, test.expectMatch, match)
		}
	}
}

func TestGetNodeOSArch(t *testing.T) {
	labels1 := map[string]string{
		"kubernetes.io/os":      "windows",
		"beta.kubernetes.io/os": "windows",
		"node1":                 "value1",
	}

	labels2 := map[string]string{
		"beta.kubernetes.io/os": "windows",
		"node1":                 "value1",
		"key1":                  "value1",
	}
	labels3 := map[string]string{
		"kubernetes.io/os":   "windows",
		"kubernetes.io/arch": "amd64",
		"node1":              "value1",
		"key1":               "value1",
	}
	type nodeOsArchTest struct {
		nodeLabels   map[string]string
		expectedOS   string
		expectedArch string
	}
	tests := []nodeOsArchTest{
		{
			labels1,
			"windows",
			"unknown",
		},
		{
			labels2,
			"windows",
			"unknown",
		},
		{
			labels3,
			"windows",
			"amd64",
		},
	}
	for _, test := range tests {
		os, arch := GetNodeOSArch(getNodeWithLabels(test.nodeLabels))
		if os != test.expectedOS {
			t.Errorf("GetNodeOSArch test failed, Nodes labels: %+v, expected OS: %s, got OS: %s.",
				test.nodeLabels, test.expectedOS, os)
		}
		if arch != test.expectedArch {
			t.Errorf("GetNodeOSArch test failed, Nodes labels: %+v, expected arch: %s, got arch: %s.",
				test.nodeLabels, test.expectedArch, arch)
		}
	}
}

func TestMapNodePoolToNodeNames(t *testing.T) {
	// node in gke pool with an additional label
	node1 := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				NodePoolGKE:     NodePoolGKE,
				"another-label": "another-label",
			},
		},
	}
	// node in EKS pool
	node2 := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node2",
			Labels: map[string]string{
				NodePoolEKSIdentifier.List()[0]: NodePoolEKSIdentifier.List()[0],
			},
		},
	}
	// node in GKE pool
	node3 := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node3",
			Labels: map[string]string{
				NodePoolGKE: NodePoolGKE,
			},
		},
	}

	// node that would be in two pools
	node4 := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node4",
			Labels: map[string]string{
				NodePoolEKSIdentifier.List()[1]: NodePoolEKSIdentifier.List()[1],
				NodePoolAKS:                     NodePoolAKS,
			},
		},
	}

	// node with no labels
	node5 := v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-node5",
			Labels: map[string]string{},
		},
	}

	nodes := []*v1.Node{&node1, &node2, &node3, &node4, &node5}

	nodePoolToNodes := MapNodePoolToNodes(nodes, map[string][]*v1.Node{})
	assert.Equal(t, map[string][]*v1.Node{
		NodePoolEKSIdentifier.List()[0]: {&node2},
		NodePoolEKSIdentifier.List()[1]: {&node4},
		NodePoolGKE:                     {&node1, &node3},
		NodePoolAKS:                     {&node4},
	}, nodePoolToNodes)
}

func getNodeWithLabels(labels map[string]string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels,
		},
	}
}
