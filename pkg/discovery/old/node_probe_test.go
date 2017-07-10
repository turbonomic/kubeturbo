package old

import (
	"k8s.io/apimachinery/pkg/types"
	api "k8s.io/client-go/pkg/api/v1"
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
