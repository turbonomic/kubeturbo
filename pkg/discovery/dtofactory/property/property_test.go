package property

import (
	"github.com/stretchr/testify/assert"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"testing"
)

const (
	label1Key   = "label1"
	label1Value = "value1"
	label2Key   = "label2"
	label2Value = "value2"

	taintAKey    = "taintA"
	taintAEffect = api.TaintEffectNoSchedule
	taintBKey    = "taintB"
	taintBValue  = "foo"
	taintBEffect = api.TaintEffectNoExecute
	taintCKey    = "taintC"
	taintCValue  = "bar"
	taintCEffect = api.TaintEffectPreferNoSchedule

	toleration1Key    = taintAKey
	toleration1Op     = api.TolerationOpExists
	toleration1Effect = taintAEffect
	toleration2Key    = taintBKey
	toleration2Op     = api.TolerationOpEqual
	toleration2Value  = taintBValue
	toleration2Effect = taintBEffect
)

func TestNodeProperty(t *testing.T) {
	labels := make(map[string]string)
	labels[label1Key] = label1Value
	labels[label2Key] = label2Value

	node := &api.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:   "my-node-1",
			UID:    "my-node-1-UID",
			Labels: labels,
		},

		Spec: api.NodeSpec{
			Taints: []api.Taint{
				{
					Key:    taintAKey,
					Effect: taintAEffect,
				},
				{
					Key:    taintBKey,
					Value:  taintBValue,
					Effect: taintBEffect,
				},
				{
					Key:    taintCKey,
					Value:  taintCValue,
					Effect: taintCEffect,
				},
			},
		},
	}

	ps := BuildNodeProperties(node)
	nodeName := GetNodeNameFromProperty(ps)

	if nodeName != node.Name {
		t.Errorf("Failed to get node name from perperties: %+v", ps)
	}

	matches := 0
	for _, p := range ps {
		if p.GetNamespace() == VCTagsPropertyNamespace {
			var expected string
			switch p.GetName() {
			case label1Key:
				expected = label1Value
			case label2Key:
				expected = label2Value
			case taintAKey:
				expected = string(taintAEffect) + " " + TaintPropertyValueSuffix
			case taintBKey:
				expected = taintBValue + " " + string(taintBEffect) + " " + TaintPropertyValueSuffix
			case taintCKey:
				expected = taintCValue + " " + string(taintCEffect) + " " + TaintPropertyValueSuffix
			default:
				continue
			}
			matches++
			assert.EqualValues(t, expected, p.GetValue())
		}
	}
	assert.Equal(t, 5, matches, "there should be 5 matches in the test node properties")
}

func TestBuildPodProperties(t *testing.T) {
	labels := make(map[string]string)
	labels[label1Key] = label1Value
	labels[label2Key] = label2Value

	pod := &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod1",
			Namespace: "my-namespace",
			UID:       "my-pod-1-UID",
			Labels:    labels,
		},
		Spec: api.PodSpec{
			Tolerations: []api.Toleration{
				{
					Key:      toleration1Key,
					Operator: toleration1Op,
					Effect:   toleration1Effect,
				},
				{
					Key:      toleration2Key,
					Operator: toleration2Op,
					Value:    toleration2Value,
					Effect:   toleration2Effect,
				},
			},
		},
	}

	ps := BuildPodProperties(pod)
	ns, name, err := GetPodInfoFromProperty(ps)
	if err != nil {
		t.Errorf("pod property test failed: %v", err)
		return
	}

	if ns != pod.Namespace {
		t.Errorf("Pod property test failed: namespace is wrong (%v) Vs. (%v)", ns, pod.Namespace)
	}

	if name != pod.Name {
		t.Errorf("Pod property test failed: pod name is wrong: (%v) Vs. (%v)", name, pod.Name)
	}

	matches := 0
	for _, p := range ps {
		if p.GetNamespace() == VCTagsPropertyNamespace {
			var expected string
			switch p.GetName() {
			case label1Key:
				expected = label1Value
			case label2Key:
				expected = label2Value
			case toleration1Key:
				expected = string(toleration1Op) + " " + string(toleration1Effect) + " " + TolerationPropertyValueSuffix
			case toleration2Key:
				expected = toleration2Value + " " + string(toleration2Op) + " " + string(toleration2Effect) + " " + TolerationPropertyValueSuffix
			default:
				continue
			}
			matches++
			assert.EqualValues(t, expected, p.GetValue())
		}
	}
	assert.Equal(t, 4, matches, "there should be 4 matches in the test pod properties")
}

func TestAddHostingPodProperties(t *testing.T) {
	namespace := "xyz"
	name := "poda"
	index := 2

	ps := AddHostingPodProperties(namespace, name, index)
	ns, nm, idx := GetHostingPodInfoFromProperty(ps)

	if ns != namespace {
		t.Error("Application perperty test failed: namespace is wrong")
	}

	if nm != name {
		t.Error("Application property test failed: pod name is wrong")
	}

	if idx != index {
		t.Error("Appliction property test failed: container index is wrong.")
	}
}
