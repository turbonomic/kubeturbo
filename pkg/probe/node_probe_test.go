package probe

import (
	// "strconv"
	// "time"
	"testing"

	"k8s.io/kubernetes/pkg/api"

	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/types"

	// vmtAdvisor "k8s.io/kubernetes/plugin/pkg/vmturbo/vmt/cadvisor"

	// "github.com/vmturbo/vmturbo-go-sdk/sdk"
	"github.com/stretchr/testify/assert"
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

func (this *FakeNodeBuilder) build() *api.Node {
	return this.node
}

func (this *FakeNodeBuilder) labels(labels map[string]string) *FakeNodeBuilder {
	this.node.Labels = labels
	return this
}

type FakeNodeGetter struct{}

func (this *FakeNodeGetter) GetNodes(label labels.Selector, field fields.Selector) []*api.Node {
	var nodesList []*api.Node
	fakenode1 := newFakeNodeBuilder("uid1", "fakenode1").build()
	nodesList = append(nodesList, fakenode1)
	fakenode2 := newFakeNodeBuilder("uid2", "fakenode2").build()
	nodesList = append(nodesList, fakenode2)
	return nodesList
}

func TestGetNodes(t *testing.T) {
	fakeGetter := &FakeNodeGetter{}

	nodeProbe := NewNodeProbe(fakeGetter.GetNodes)

	nodes := nodeProbe.GetNodes(nil, nil)

	assert := assert.New(t)
	assert.Equal(2, len(nodes))
}
