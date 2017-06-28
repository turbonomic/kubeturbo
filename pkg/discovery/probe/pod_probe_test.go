package probe

import (
	"k8s.io/apimachinery/pkg/types"
	api "k8s.io/client-go/pkg/api/v1"
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
