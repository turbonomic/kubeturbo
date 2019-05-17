package executor

import (
	"fmt"
	"math"

	"github.com/golang/glog"

	k8sapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/turbonomic/kubeturbo/pkg/action/util"
	idutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	podutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	epsilon        float64 = 0.001
	smallestAmount float64 = 1.0
)

type containerResizeSpec struct {
	// the new capacity of the resources
	NewCapacity k8sapi.ResourceList
	NewRequest  k8sapi.ResourceList

	// index of Pod's containers
	Index int
}

type ContainerResizer struct {
	TurboK8sActionExecutor
	kubeletClient              *kubeclient.KubeletClient
	k8sVersion                 string
	noneSchedulerName          string
	enableNonDisruptiveSupport bool
	sccAllowedSet              map[string]struct{}
	spec                       *containerResizeSpec
}

func NewContainerResizeSpec(idx int) *containerResizeSpec {
	return &containerResizeSpec{
		Index:       idx,
		NewCapacity: make(k8sapi.ResourceList),
		NewRequest:  make(k8sapi.ResourceList),
	}
}

func NewContainerResizer(ae TurboK8sActionExecutor, kubeletClient *kubeclient.KubeletClient,
	sccAllowedSet map[string]struct{}) *ContainerResizer {
	return &ContainerResizer{
		TurboK8sActionExecutor: ae,
		kubeletClient:          kubeletClient,
		sccAllowedSet:          sccAllowedSet,
	}
}

// get node cpu frequency, in KHz;
func (r *ContainerResizer) getNodeCPUFrequency(host string) (uint64, error) {
	// always access kubelet via node IP
	node, err := util.GetNodebyName(r.kubeClient, host)
	if err != nil {
		return 1, fmt.Errorf("failed to get node by name: %v", err)
	}

	ip, err := podutil.GetNodeIP(node)
	if err != nil {
		return 1, fmt.Errorf("failed to get IP of node %v: %v", node, err)
	}

	return r.kubeletClient.GetMachineCpuFrequency(ip)
}

func (r *ContainerResizer) setCPUQuantity(cpuMhz float64, host string, rlist k8sapi.ResourceList) error {
	cpuFrequency, err := r.getNodeCPUFrequency(host)
	if err != nil {
		return fmt.Errorf("failed to get node[%s] cpu frequency: %v", host, err)
	}

	cpuQuantity, err := genCPUQuantity(cpuMhz, cpuFrequency)
	if err != nil {
		return fmt.Errorf("failed to generate CPU quantity: %v", err)
	}

	rlist[k8sapi.ResourceCPU] = cpuQuantity
	return nil
}

func (r *ContainerResizer) buildResourceList(pod *k8sapi.Pod, cType proto.CommodityDTO_CommodityType,
	amount float64, result k8sapi.ResourceList) error {
	switch cType {
	case proto.CommodityDTO_VCPU:
		host := pod.Spec.NodeName
		err := r.setCPUQuantity(amount, host, result)
		if err != nil {
			return fmt.Errorf("failed to build cpu.Capacity: %v", err)
		}
	case proto.CommodityDTO_VMEM:
		memory, err := genMemoryQuantity(amount)
		if err != nil {
			return fmt.Errorf("failed to build mem.Capacity: %v", err)
		}
		result[k8sapi.ResourceMemory] = memory
	default:
		return fmt.Errorf("unsupport Commodity type %v", cType)
	}
	return nil
}

func getNewAmount(current, new float64) (bool, float64) {
	delta := math.Abs(current - new)
	if delta < epsilon {
		return false, 0
	}
	if new < smallestAmount {
		glog.Warningf("commodity amount is too small %v, reset to %v", new, smallestAmount)
		return true, smallestAmount
	}
	return true, new
}

// get commodity type and new capacity, and convert it into a k8s.Quantity.
func (r *ContainerResizer) buildResourceLists(pod *k8sapi.Pod, actionItem *proto.ActionItemDTO,
	spec *containerResizeSpec) error {
	comm1 := actionItem.GetCurrentComm()
	comm2 := actionItem.GetNewComm()

	if comm1.GetCommodityType() != comm2.GetCommodityType() {
		return fmt.Errorf("commodity type does not match %v vs %v",
			comm1.CommodityType.String(), comm2.CommodityType.String())
	}

	cType := comm2.GetCommodityType()

	//1. check capacity change
	change, amount := getNewAmount(comm1.GetCapacity(), comm2.GetCapacity())
	if change {
		if err := r.buildResourceList(pod, cType, amount, spec.NewCapacity); err != nil {
			return fmt.Errorf("failed to build resource list when resize %v capacity to %v: %v",
				cType.String(), amount, err)
		}
		glog.V(3).Infof("Resize pod-%v %v Capacity to %v", pod.Name, cType.String(), amount)
	}

	//2. check reservation
	change, amount = getNewAmount(comm1.GetReservation(), comm2.GetReservation())
	if change {
		if err := r.buildResourceList(pod, cType, amount, spec.NewRequest); err != nil {
			return fmt.Errorf("failed to build resource list when resize %v reservation to %v: %v",
				cType.String(), amount, err)
		}
		glog.V(3).Infof("Resize pod-%v %v Reservation to %v", pod.Name, cType.String(), amount)
	}

	return nil
}

// fix-OM-30019: when resizing Capacity, if request is not specified, then the request will be equal to the new capacity.
// Solution: if request is not specified, then set it to zero.
func (r *ContainerResizer) setZeroRequest(pod *k8sapi.Pod, containerIdx int, spec *containerResizeSpec) {
	// check whether Capacity resizing is involved.
	if len(spec.NewCapacity) < 1 {
		glog.V(3).Infof("No capacity resizing, no need to set request.")
		return
	}

	// for each type of resource Capacity resizing, check whether the request is specified
	container := &(pod.Spec.Containers[containerIdx])
	origRequest := container.Resources.Requests
	if origRequest == nil {
		origRequest = make(k8sapi.ResourceList)
		container.Resources.Requests = origRequest
	}
	zero := resource.NewQuantity(0, resource.BinarySI)

	for k := range spec.NewCapacity {
		if _, exist := spec.NewRequest[k]; exist {
			continue
		}

		if _, exist := origRequest[k]; !exist {
			spec.NewRequest[k] = *zero
			glog.V(3).Infof("Set unspecified %v request to zero", k)
		}
	}
}

func (r *ContainerResizer) buildResizeSpec(actionItem *proto.ActionItemDTO, pod *k8sapi.Pod) (*containerResizeSpec, error) {

	// get hosting Pod and containerIndex
	entity := actionItem.GetTargetSE()
	containerId := entity.GetId()

	_, containerIndex, err := idutil.ParseContainerId(containerId)
	if err != nil {
		return nil, fmt.Errorf("failed to parse container index to build resizeAction: %v", err)
	}

	if containerIndex < 0 || containerIndex > len(pod.Spec.Containers) {
		return nil, fmt.Errorf("invalid containerIndex %d", containerIndex)
	}

	// build the new resource requirements
	resizeSpec := NewContainerResizeSpec(containerIndex)
	if err = r.buildResourceLists(pod, actionItem, resizeSpec); err != nil {
		return nil, fmt.Errorf("failed to build resizeSpec: %v", err)
	}

	// set request to 0 if not specified
	r.setZeroRequest(pod, containerIndex, resizeSpec)

	// check if the resize spec is empty
	if len(resizeSpec.NewCapacity) < 1 && len(resizeSpec.NewRequest) < 1 {
		return nil, fmt.Errorf("resize specification is empty")
	}

	return resizeSpec, nil
}

// Execute executes the container resize action
// The error info will be shown in UI
func (r *ContainerResizer) Execute(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	actionItem := input.ActionItem
	pod := input.Pod

	// check if the pod privilege is supported
	if !util.SupportPrivilegePod(pod, r.sccAllowedSet) {
		err := fmt.Errorf("pod %s/%s has unsupported SCC", pod.Namespace, pod.Name)
		glog.Errorf("Failed to execute resize action: %v", err)
		return &TurboActionExecutorOutput{}, err
	}

	// build resize specification
	spec, err := r.buildResizeSpec(actionItem, pod)
	if err != nil {
		glog.Errorf("Failed to execute resize action: %v", err)
		return &TurboActionExecutorOutput{}, err
	}

	// execute the Action
	npod, err := resizeContainer(
		r.kubeClient,
		pod,
		spec,
		actionItem.GetConsistentScalingCompliance(),
	)
	if err != nil {
		glog.Errorf("Failed to execute resize action: %v", err)
		return &TurboActionExecutorOutput{}, err
	}

	return &TurboActionExecutorOutput{
		Succeeded: true,
		OldPod:    pod,
		NewPod:    npod,
	}, nil
}
