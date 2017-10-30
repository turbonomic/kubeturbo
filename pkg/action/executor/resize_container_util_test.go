package executor

import (
	"fmt"
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/api/resource"
	k8sapi "k8s.io/client-go/pkg/api/v1"
	"testing"
)

func TestGenCPUQuantity(t *testing.T) {
	nodeCPUfreq := uint64(2600000) //khz
	reqArray := []int{2600, 1300, 0, 5200}
	expectArray := []int{1000, 500, 1, 2000}

	for i, req := range reqArray {
		q, err := genCPUQuantity(float64(req), nodeCPUfreq)
		if err != nil {
			t.Error(err)
		}

		rq := q.MilliValue()
		expect := int64(expectArray[i])
		if rq != expect {
			t.Errorf("failed to convert CPU quantity: %d Vs. %d", rq, expect)
		}
	}
}

func TestGenMemoryQuantity(t *testing.T) {
	mem := float64(1024) // KB
	expect := int64(1024 * 1024)

	q, err := genMemoryQuantity(mem)
	if err != nil {
		t.Error(err)
	}

	rmem := q.Value()
	if rmem != expect {
		t.Errorf("failed: %d Vs. %d", rmem, expect)
	}
}

func TestGenMemoryQuantity2(t *testing.T) {
	inputArray := []int64{1, 1024, 2048, 65536}
	expectArray := []int64{1024, 1024 * 1024, 2048 * 1024, 65536 * 1024}

	for i, input := range inputArray {
		q, err := genMemoryQuantity(float64(input))
		if err != nil {
			t.Error(err)
		}

		rq := q.Value()
		expect := expectArray[i]
		if rq != expect {
			t.Errorf("failed: %d Vs. %d", rq, expect)
		}
	}
}

func generateResourceList(cpuMillisecond, memMB int) (k8sapi.ResourceList, error) {
	alist := make(k8sapi.ResourceList)

	cpuQ, err := resource.ParseQuantity(fmt.Sprintf("%dm", cpuMillisecond))
	if err != nil {
		glog.Errorf("failed to parse cpu Quantity: %v", err)
		return alist, err
	}
	memQ, err := resource.ParseQuantity(fmt.Sprintf("%dMi", memMB))
	if err != nil {
		glog.Errorf("failed to parse memory Quantity: %v", err)
		return alist, err
	}

	alist[k8sapi.ResourceCPU] = cpuQ
	alist[k8sapi.ResourceMemory] = memQ

	return alist, nil
}

func setContainerResourceLimit(container *k8sapi.Container, cpuMillisecond, memMB int) {
	rlist, err := generateResourceList(cpuMillisecond, memMB)
	if err != nil {
		return
	}

	if container.Resources.Limits == nil {
		container.Resources.Limits = make(k8sapi.ResourceList)
	}

	for k, v := range rlist {
		container.Resources.Limits[k] = v
	}
}

func printResourceList(rmap k8sapi.ResourceList) {
	for k, v := range rmap {
		fmt.Printf("k=%s, v=%++v\n", k, v)
	}
}

func compareResourceList(rmap k8sapi.ResourceList, cpuMillisecond, memMB int) error {
	cpuQ := rmap[k8sapi.ResourceCPU]
	memQ := rmap[k8sapi.ResourceMemory]

	if cpuQ.MilliValue() != int64(cpuMillisecond) {
		return fmt.Errorf("cpu value misMatch: %d Vs. %d", cpuQ.MilliValue(), cpuMillisecond)
	}

	mem := memQ.Value() / int64(1024*1024)
	if mem != int64(memMB) {
		return fmt.Errorf("memory value misMatch: %d Vs. %d", cpuQ.MilliValue(), cpuMillisecond)
	}

	return nil
}

func TestUpdateCapacity(t *testing.T) {
	pod := &(k8sapi.Pod{})

	pod.Spec.Containers = append(pod.Spec.Containers, k8sapi.Container{})
	if len(pod.Spec.Containers) != 1 {
		t.Errorf("unable to add container to Pod.")
	}

	container := &(pod.Spec.Containers[0])
	container.Name = "hello"

	//printResourceList(container.Resources.Limits)
	setContainerResourceLimit(container, 300, 410)
	//printResourceList(container.Resources.Limits)
	if err := compareResourceList(container.Resources.Limits, 300, 410); err != nil {
		t.Error(err)
	}

	patch, err := generateResourceList(250, 500)
	if err != nil {
		t.Errorf("unable to test: %v", err)
	}

	//c := NewContainerResizer(nil, nil, "1.5", "aa", nil)
	updateCapacity(container, patch)
	if err := compareResourceList(container.Resources.Limits, 250, 500); err != nil {
		t.Error(err)
	}

	//fmt.Printf("after update\n")
	//printResourceList(container.Resources.Limits)
	//printResourceList(container.Resources.Requests)
}
