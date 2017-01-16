package probe

import (
	"testing"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/types"
)

type FakeNodeBuilder struct {
	node *api.Node
}

func newFakeNodeBuilder(uid types.UID, name string) *FakeNodeBuilder {
	node := new(api.Node)
	node.UID = uid
	node.Name = name

	return &FakeNodeBuilder{
		node: node,
	}
}

func (fnb *FakeNodeBuilder) build() *api.Node {
	return fnb.node
}

func (fnb *FakeNodeBuilder) labels(labels map[string]string) *FakeNodeBuilder {
	fnb.node.Labels = labels
	return fnb
}

type FakeNodeGetter struct{}

func (fng *FakeNodeGetter) GetNodes(label labels.Selector, field fields.Selector) []*api.Node {
	var nodesList []*api.Node
	fakenode1 := newFakeNodeBuilder("uid1", "fakenode1").build()
	nodesList = append(nodesList, fakenode1)
	fakenode2 := newFakeNodeBuilder("uid2", "fakenode2").build()
	nodesList = append(nodesList, fakenode2)
	return nodesList
}

func TestGetNodes(t *testing.T) {
	fakeGetter := &FakeNodeGetter{}

	nodeProbe := NewNodeProbe(fakeGetter.GetNodes, nil)

	nodes := nodeProbe.GetNodes(nil, nil)
	if len(nodes) != 2 {
		t.Errorf("Expected %n nodes, got %n", 2, len(nodes))
	}
}
