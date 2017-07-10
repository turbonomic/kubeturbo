package util

import (
	"errors"
	"fmt"

	api "k8s.io/client-go/pkg/api/v1"
)

func GetNodeResourceRequestConsumption(pods []*api.Pod) (nodeCpuProvisionedUsedCore, nodeMemoryProvisionedUsedKiloBytes float64, err error) {
	if pods == nil {
		err = errors.New("pod list passed in is nil")
		return
	}
	for _, pod := range pods {
		podCpuRequest, podMemoryRequest, getErr := GetPodResourceRequest(pod)
		if getErr != nil {
			err = fmt.Errorf("failed to get resource requests: %s", getErr)
			return
		}
		nodeCpuProvisionedUsedCore += podCpuRequest
		nodeMemoryProvisionedUsedKiloBytes += podMemoryRequest
	}
	return
}

// CPU returned is in KHz; Mem is in Kb
func GetPodResourceLimits(pod *api.Pod) (cpuCapacity float64, memCapacity float64, err error) {
	if pod == nil {
		err = errors.New("pod passed in is nil.")
		return
	}

	for _, container := range pod.Spec.Containers {
		limits := container.Resources.Limits
		ctnCpuCap, ctnMemoryCap := GetCpuAndMemoryValues(limits)
		cpuCapacity += ctnCpuCap
		memCapacity += ctnMemoryCap
	}
	return
}

// CPU returned is in KHz; Mem is in Kb
func GetPodResourceRequest(pod *api.Pod) (cpuRequest float64, memRequest float64, err error) {
	if pod == nil {
		err = errors.New("pod passed in is nil.")
		return
	}

	for _, container := range pod.Spec.Containers {
		request := container.Resources.Requests
		ctnCpuRequest, ctnMemoryRequest := GetCpuAndMemoryValues(request)
		cpuRequest += ctnCpuRequest
		memRequest += ctnMemoryRequest
	}
	return
}

// CPU returned is in core; Memory is in Kb
func GetCpuAndMemoryValues(resource api.ResourceList) (cpuCapacityCore, memoryCapacityKiloBytes float64) {
	ctnMemoryCapacityBytes := resource.Memory().Value()
	ctnCpuCapacityMilliCore := resource.Cpu().MilliValue()
	memoryCapacityKiloBytes += float64(ctnMemoryCapacityBytes) / KilobytesToBytes
	cpuCapacityCore += float64(ctnCpuCapacityMilliCore) / MilliToUnit

	return
}
