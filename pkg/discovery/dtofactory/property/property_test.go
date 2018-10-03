package property

import (
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	api "k8s.io/api/core/v1"

	"testing"
)

func TestNodeProperty(t *testing.T) {
	labels := make(map[string]string)
	labels["label1"] = "value1"
	labels["label2"] = "valuel2"

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
	}

	property := BuildNodeProperties(node)
	ps := []*proto.EntityDTO_EntityProperty{property}

	nodeName := GetNodeNameFromProperty(ps)

	if nodeName != node.Name {
		t.Errorf("Failed to get node name from perperties: %+v", ps)
	}
}

func TestBuildPodProperties(t *testing.T) {
	labels := make(map[string]string)
	labels["label1"] = "value1"
	labels["label2"] = "valuel2"

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
