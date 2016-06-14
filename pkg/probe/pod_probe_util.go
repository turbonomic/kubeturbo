package probe

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"
)

// CPU returned is in KHz; Mem is in Kb
func GetResourceLimits(pod *api.Pod) (cpuCapacity float64, memCapacity float64, err error) {
	if pod == nil {
		return 0, 0, fmt.Errorf("pod passed in is nil.")
	}

	for _, container := range pod.Spec.Containers {
		limits := container.Resources.Limits
		request := container.Resources.Requests

		memCap := limits.Memory().Value()
		if memCap == 0 {
			memCap = request.Memory().Value()
		}
		cpuCap := limits.Cpu().MilliValue()
		if cpuCap == 0 {
			cpuCap = request.Cpu().MilliValue()
		}
		memCapacity += float64(memCap) / 1024
		cpuCapacity += float64(cpuCap) / 1000
	}
	return
}
