package probe

import (
	"testing"

	api "k8s.io/client-go/pkg/api/v1"
	"k8s.io/apimachinery/pkg/types"
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

func (fng *FakeNodeGetter) GetNodes(label, field string) ([]*api.Node, error) {
	var nodesList []*api.Node
	fakenode1 := newFakeNodeBuilder("uid1", "fakenode1").build()
	nodesList = append(nodesList, fakenode1)
	fakenode2 := newFakeNodeBuilder("uid2", "fakenode2").build()
	nodesList = append(nodesList, fakenode2)
	return nodesList, nil
}

func TestGetNodes(t *testing.T) {
	fakeGetter := &FakeNodeGetter{}

	nodeProbe := NewNodeProbe(fakeGetter.GetNodes, nil, nil)

	nodes, err := nodeProbe.GetNodes("", "")
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	if len(nodes) != 2 {
		t.Errorf("Expected %n nodes, got %n", 2, len(nodes))
	}
}
