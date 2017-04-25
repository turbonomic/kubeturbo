package probe

import (
	"testing"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/fields"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/pkg/types"

)

type FakePodBuilder struct {
	pod *api.Pod
}

func newFakePodBuilder(uid types.UID, name string) *FakePodBuilder {
	pod := new(api.Pod)
	pod.UID = uid
	pod.Name = name

	return &FakePodBuilder{
		pod: pod,
	}
}

func (this *FakePodBuilder) build() *api.Pod {
	return this.pod
}

func (this *FakePodBuilder) labels(labels map[string]string) *FakePodBuilder {
	this.pod.Labels = labels
	return this
}

type FakePodGetter struct{}

func (fpg *FakePodGetter) GetPods(namespace string, label labels.Selector, field fields.Selector) ([]*api.Pod, error) {
	var podsList []*api.Pod
	fakePod1 := newFakePodBuilder("uid1", "fakePod1").build()
	podsList = append(podsList, fakePod1)
	fakePod2 := newFakePodBuilder("uid2", "fakePod2").build()
	podsList = append(podsList, fakePod2)
	return podsList, nil
}

func TestGetPods(t *testing.T) {
	fakeGetter := &FakePodGetter{}

	podProbe := NewPodProbe(fakeGetter.GetPods, nil)

	pods, _ := podProbe.GetPods("", nil, nil)

	if len(pods) != 2 {
		t.Errorf("Expected %n nodes, got %n", 2, len(pods))
	}
}
