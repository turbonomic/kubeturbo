package util

import (
	"errors"
	"math"
	"net"

	"github.com/golang/glog"
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

// Gets the allocatable number of pods from the node resource
func GetNumPodsAllocatable(node *api.Node) float64 {
	// Compute both the available IP address range and the maxpods set on the node.
	// Pick the smaller limit.
	var allocatableIPAddresses float64
	if node.Spec.PodCIDR != "" {
		// This also validates the CIDR string for malformed values, although unlikely.
		_, podNet, err := net.ParseCIDR(node.Spec.PodCIDR)
		if err != nil {
			glog.Warning("Error parsing node CIDR:", err)
		} else {
			allocatableIPAddresses = getAllocatableIPAddresses(podNet)
			glog.V(4).Infof("Allocatable pod IP addresses: %f on node: %s", allocatableIPAddresses, node.Name)
		}
	}

	allocatablePods := float64(node.Status.Allocatable.Pods().Value())
	glog.V(4).Infof("Allocatable maxPods: %f on node: %s", allocatablePods, node.Name)

	if allocatableIPAddresses != 0 && allocatableIPAddresses < allocatablePods {
		return allocatableIPAddresses
	}
	return allocatablePods
}

// Gets the number of allocatable addresses in the subnet
// TODO: Validate if this works fine for IPV6 addresses
func getAllocatableIPAddresses(n *net.IPNet) float64 {
	ones, bits := n.Mask.Size()
	// Ref: https://erikberg.com/notes/networks.html
	return (math.Pow(2, float64(bits-ones)) - 2)
}
