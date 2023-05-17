package executor

import (
	"testing"
	"github.com/stretchr/testify/assert"
	k8sapi "k8s.io/api/core/v1"
)

func TestBuildDesiredPod4QuotaEvaluation(t *testing.T) {
	pod := createPod()
	idx := 0

	var resizeSpecs []*containerResizeSpec
	rMemorytype := k8sapi.ResourceMemory
	rCPUtype := k8sapi.ResourceCPU
	spec := NewContainerResizeSpec(idx)
	cpuAmount, err := genCPUQuantity(1200.0)
	memoryAmount, err := genMemoryQuantity(128.0)
	if err != nil {
		t.Errorf("Failed to generate memory Quantity: %v", err)
		return
	}
	spec.NewCapacity[rMemorytype] = memoryAmount
	spec.NewCapacity[rCPUtype] = cpuAmount
	resizeSpecs = append(resizeSpecs, spec)
	namespace := "quotaresize"
	podTemplate := buildDesiredPod4QuotaEvaluation(namespace, resizeSpecs, pod.Spec)
	assert.Equal(t, podTemplate.Spec.Containers[spec.Index].Resources.Limits[k8sapi.ResourceMemory], memoryAmount)
	assert.Equal(t, podTemplate.Spec.Containers[spec.Index].Resources.Limits[k8sapi.ResourceCPU], cpuAmount)

}
