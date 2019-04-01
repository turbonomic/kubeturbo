package util

import (
	"errors"

	api "k8s.io/api/core/v1"
)

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

// CPU returned is in core; Memory is in Kb
func GetCpuAndMemoryValues(resource api.ResourceList) (cpuCapacityCore, memoryCapacityKiloBytes float64) {
	ctnMemoryCapacityBytes := resource.Memory().Value()
	ctnCpuCapacityMilliCore := resource.Cpu().MilliValue()
	memoryCapacityKiloBytes = float64(ctnMemoryCapacityBytes) / KilobytesToBytes
	cpuCapacityCore = float64(ctnCpuCapacityMilliCore) / MilliToUnit

	return
}
