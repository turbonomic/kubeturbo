package executor

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestPodValidateLimitFunc(t *testing.T) {
	pod := validPod("testpod", 1, getResourceRequirements(getComputeResourceList("10m", "10Mi"), getComputeResourceList("20m", "20Mi")))

	// violate the max
	limitrange := createLimitRange(api.LimitTypeContainer, getComputeResourceList("2m", "2Mi"), getComputeResourceList("15m", "15Mi"), api.ResourceList{}, api.ResourceList{}, api.ResourceList{})
	err := PodValidateLimitFunc(&limitrange, &pod)
	assert.NotNil(t, err)

	// viloate the min
	limitrange = createLimitRange(api.LimitTypeContainer, getComputeResourceList("15m", "15Mi"), getComputeResourceList("50m", "50Mi"), api.ResourceList{}, api.ResourceList{}, api.ResourceList{})
	err = PodValidateLimitFunc(&limitrange, &pod)
	assert.NotNil(t, err)

	// no viloation
	limitrange = createLimitRange(api.LimitTypeContainer, getComputeResourceList("2m", "2Mi"), getComputeResourceList("50m", "50Mi"), api.ResourceList{}, api.ResourceList{}, api.ResourceList{})
	err = PodValidateLimitFunc(&limitrange, &pod)
	assert.Nil(t, err)
}

// createLimitRange creates a limit range with the specified data
func createLimitRange(limitType api.LimitType, min, max, defaultLimit, defaultRequest, maxLimitRequestRatio api.ResourceList) api.LimitRange {
	return api.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "abc",
			Namespace: "test",
		},
		Spec: api.LimitRangeSpec{
			Limits: []api.LimitRangeItem{
				{
					Type:                 limitType,
					Min:                  min,
					Max:                  max,
					Default:              defaultLimit,
					DefaultRequest:       defaultRequest,
					MaxLimitRequestRatio: maxLimitRequestRatio,
				},
			},
		},
	}
}

func validPod(name string, numContainers int, resources api.ResourceRequirements) api.Pod {
	pod := api.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test"},
		Spec:       api.PodSpec{},
	}
	pod.Spec.Containers = make([]api.Container, 0, numContainers)
	for i := 0; i < numContainers; i++ {
		pod.Spec.Containers = append(pod.Spec.Containers, api.Container{
			Image:     "foo:V" + strconv.Itoa(i),
			Resources: resources,
			Name:      "foo-" + strconv.Itoa(i),
		})
	}
	return pod
}

func getComputeResourceList(cpu, memory string) api.ResourceList {
	res := api.ResourceList{}
	if cpu != "" {
		res[api.ResourceCPU] = resource.MustParse(cpu)
	}
	if memory != "" {
		res[api.ResourceMemory] = resource.MustParse(memory)
	}
	return res
}

func getResourceRequirements(requests, limits api.ResourceList) api.ResourceRequirements {
	res := api.ResourceRequirements{}
	res.Requests = requests
	res.Limits = limits
	return res
}
