package util

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodeMatchesLabels(t *testing.T) {
	defaultExcludeLabels := map[string]string{
		"kubernetes.io/os":      "windows",
		"beta.kubernetes.io/os": "windows",
	}

	windowsLabels1 := map[string]string{
		"kubernetes.io/os":      "windows",
		"beta.kubernetes.io/os": "windows",
		"node1":                 "value1",
	}

	windowsLabels2 := map[string]string{
		"beta.kubernetes.io/os": "windows",
		"node1":                 "value1",
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

	customLabels := map[string]string{
		"key1": "value1",
		"key2": "value2",
	}

	type labelTest struct {
		mustMatchLabels map[string]string
		node            *v1.Node
		expectMatch     bool
	}

	tests := []labelTest{
		{
			mustMatchLabels: nil,
			node:            getNodeWithLabels(windowsLabels1),
			expectMatch:     true,
		},
		{
			mustMatchLabels: nil,
			node:            getNodeWithLabels(windowsLabels2),
			expectMatch:     true,
		},
		{
			mustMatchLabels: nil,
			node:            getNodeWithLabels(linuxLabels),
			expectMatch:     false,
		},
		{
			mustMatchLabels: defaultExcludeLabels,
			node:            getNodeWithLabels(windowsLabels1),
			expectMatch:     true,
		},
		{
			mustMatchLabels: defaultExcludeLabels,
			node:            getNodeWithLabels(windowsLabels2),
			expectMatch:     false,
		},
		{
			mustMatchLabels: defaultExcludeLabels,
			node:            getNodeWithLabels(linuxLabels),
			expectMatch:     false,
		},
		{
			mustMatchLabels: customLabels,
			node:            getNodeWithLabels(windowsLabels3),
			expectMatch:     true,
		},
	}

	for _, test := range tests {
		match := NodeMatchesLabels(test.node, defaultExcludeLabels, test.mustMatchLabels)
		if match != test.expectMatch {
			t.Errorf("Label match failed, Nodes labels: %++v, excludeLabels: %++v, mustMatchLabels: %++v."+
				"Expected match: %v, got: %v", test.node.Labels, defaultExcludeLabels, test.mustMatchLabels, test.expectMatch, match)
		}
	}
}

func getNodeWithLabels(labels map[string]string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Labels: labels,
		},
	}
}
