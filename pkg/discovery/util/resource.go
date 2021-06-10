package util

import (
	"math"
	"net"

	"github.com/golang/glog"
	api "k8s.io/api/core/v1"
)

// CPU returned is in core; Memory is in Kb
func GetCpuAndMemoryValues(resource api.ResourceList) (ctnCpuCapacityMilliCore, memoryCapacityKiloBytes float64) {
	ctnCpuCapacityMilliCore = float64(resource.Cpu().MilliValue())
	ctnMemoryCapacityBytes := resource.Memory().Value()
	memoryCapacityKiloBytes = Base2BytesToKilobytes(float64(ctnMemoryCapacityBytes))

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
