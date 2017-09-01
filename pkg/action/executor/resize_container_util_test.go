package executor

import (
	"testing"
	"fmt"
	"k8s.io/apimachinery/pkg/api/resource"
	k8sapi "k8s.io/client-go/pkg/api/v1"
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
	expect := int64(1024*1024)

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
	expectArray := []int64{1024, 1024*1024, 2048*1024, 65536*1024}

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

func generateResourceList(cpuMillisecond, memMB int) k8sapi.ResourceList {

	cpuQ := resource.MustParse(fmt.Sprintf("%dm", cpuMillisecond))
	memQ := resource.MustParse(fmt.Sprintf("%dMi", memMB))

	fmt.Printf("cpuQ: %+v\n", cpuQ)
	fmt.Printf("memQ: %+v\n", memQ)

	alist := make(k8sapi.ResourceList)
	alist[k8sapi.ResourceCPU] = cpuQ
	alist[k8sapi.ResourceMemory] = memQ

	return alist
}

func setContainerResourceLimit(container *k8sapi.Container, cpuMillisecond, memMB int) {
	rlist := generateResourceList(cpuMillisecond, memMB)
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

func TestUpdateCapacity(t *testing.T) {
	pod := &(k8sapi.Pod{})

	fmt.Printf("containerNum: %d\n", len(pod.Spec.Containers))
	pod.Spec.Containers = append(pod.Spec.Containers, k8sapi.Container{})
	fmt.Printf("containerNum: %d\n", len(pod.Spec.Containers))

	container := &(pod.Spec.Containers[0])
	container.Name = "hello"

	fmt.Println(len(container.Resources.Limits))
	setContainerResourceLimit(container, 300, 410)
	fmt.Println(len(container.Resources.Limits))
	printResourceList(container.Resources.Limits)

	patch := generateResourceList(250, 500)

	//c := NewContainerResizer(nil, nil, "1.5", "aa", nil)
	updateCapacity(pod, 0, patch)

	fmt.Printf("after update\n")
	printResourceList(container.Resources.Limits)
	printResourceList(container.Resources.Requests)
}