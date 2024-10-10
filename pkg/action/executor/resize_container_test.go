package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	k8sapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func createPod() *k8sapi.Pod {
	pod := &k8sapi.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name: "my-pod-1",
			UID:  "my-pod-1-UID",
		},

		Spec: k8sapi.PodSpec{},
	}

	resourceSpec := k8sapi.ResourceRequirements{
		Limits:   make(k8sapi.ResourceList),
		Requests: make(k8sapi.ResourceList),
	}

	container := k8sapi.Container{
		Resources: resourceSpec,
	}

	containers := []k8sapi.Container{container}
	pod.Spec.Containers = containers

	return pod
}

func TestBuildResourceListsWithLimits(t *testing.T) {
	actionItem := &proto.ActionItemDTO{
		CurrentComm: mockCommodity(proto.CommodityDTO_VMEM, 10),
		NewComm:     mockCommodity(proto.CommodityDTO_VMEM, 20),
	}
	spec := NewContainerResizeSpec(0)
	resizer := &ContainerResizer{}
	resizer.buildResourceLists(createPod().Name, actionItem, spec)

	assert.Equal(t, 0, len(spec.NewRequest))
	newMemoryCapacity := spec.NewCapacity[k8sapi.ResourceMemory]
	assert.Equal(t, "20Ki", newMemoryCapacity.String())
}

func TestBuildResourceListsWithRequests(t *testing.T) {
	actionItem := &proto.ActionItemDTO{
		CurrentComm: mockCommodity(proto.CommodityDTO_VMEM_REQUEST, 10),
		NewComm:     mockCommodity(proto.CommodityDTO_VMEM_REQUEST, 20),
	}
	spec := NewContainerResizeSpec(0)
	resizer := &ContainerResizer{}
	resizer.buildResourceLists(createPod().Name, actionItem, spec)

	assert.Equal(t, 0, len(spec.NewCapacity))
	newMemoryRequests := spec.NewRequest[k8sapi.ResourceMemory]
	assert.EqualValues(t, "20Ki", newMemoryRequests.String())
}

func TestGenCPUAndMemQuantity(t *testing.T) {
	amount, _ := genCPUQuantity(1.9999)
	assert.Equal(t, "2m", amount.String())
	amount, _ = genMemoryQuantity(1.9999)
	assert.Equal(t, "2Ki", amount.String())
	amount, _ = genCPUQuantity(1.001)
	assert.Equal(t, "1m", amount.String())
	amount, _ = genMemoryQuantity(1.001)
	assert.Equal(t, "1Ki", amount.String())
}

func mockCommodity(commodityType proto.CommodityDTO_CommodityType, capacity float64) *proto.CommodityDTO {
	return &proto.CommodityDTO{
		CommodityType: &commodityType,
		Capacity:      &capacity,
	}
}
