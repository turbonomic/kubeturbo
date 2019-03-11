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

	spec *containerResizeSpec
}

func NewContainerResizeSpec(idx int) *containerResizeSpec {
	return &containerResizeSpec{
		Index:       idx,
		NewCapacity: make(k8sapi.ResourceList),
		NewRequest:  make(k8sapi.ResourceList),
	}
}

func NewContainerResizer(ae TurboK8sActionExecutor, kubeletClient *kubeclient.KubeletClient, sccAllowedSet map[string]struct{}) *ContainerResizer {
	return &ContainerResizer{
		TurboK8sActionExecutor: ae,
		kubeletClient:          kubeletClient,
		sccAllowedSet:          sccAllowedSet,
	}
}

// get node cpu frequency, in KHz;
func (r *ContainerResizer) getNodeCPUFrequency(host string) (uint64, error) {
	result, err := r.kubeletClient.GetMachineCpuFrequency(host)
	if err == nil {
		return result, nil
	}
	glog.Warningf("Failed to get node node cpuFrequency by hostname: %v, will try to get it by IP.", err)

	node, err := util.GetNodebyName(r.kubeClient, host)
	if err != nil {
		glog.Errorf("failed to get node by name: %v", err)
		return 1, err
	}

	ip, err := podutil.GetNodeIP(node)
	if err != nil {
		glog.Errorf("Failed to get node IP: %v, %++v", err, node)
		return 1, err
	}

	return r.kubeletClient.GetMachineCpuFrequency(ip)
}

func (r *ContainerResizer) setCPUQuantity(cpuMhz float64, host string, rlist k8sapi.ResourceList) error {
	cpuFrequency, err := r.getNodeCPUFrequency(host)
	if err != nil {
		glog.Errorf("failed to get node[%s] cpu frequency: %v", host, err)
		return err
	}

	cpuQuantity, err := genCPUQuantity(cpuMhz, cpuFrequency)
	if err != nil {
		glog.Errorf("failed to generate CPU quantity: %v", err)
		return err
	}

	rlist[k8sapi.ResourceCPU] = cpuQuantity
	return nil
}

func (r *ContainerResizer) buildResourceList(pod *k8sapi.Pod, ctype proto.CommodityDTO_CommodityType, amount float64, result k8sapi.ResourceList) error {
	switch ctype {
	case proto.CommodityDTO_VCPU:
		host := pod.Spec.NodeName
		err := r.setCPUQuantity(amount, host, result)
		if err != nil {
			glog.Errorf("failed to build cpu.Capacity: %v", err)
			return err
		}
	case proto.CommodityDTO_VMEM:
		memory, err := genMemoryQuantity(amount)
		if err != nil {
			glog.Errorf("failed to build mem.Capacity: %v", err)
			return err
		}
		result[k8sapi.ResourceMemory] = memory
	default:
		err := fmt.Errorf("Unsupport Commodity type[%v]", ctype)
		glog.Error(err)
		return err
	}

	return nil
}

// get commodity type and new capacity, and convert it into a k8s.Quantity.
func (r *ContainerResizer) buildResourceLists(pod *k8sapi.Pod, actionItem *proto.ActionItemDTO, spec *containerResizeSpec) error {
	glog.V(4).Infof("action=%+++v", actionItem)
	comm1 := actionItem.GetCurrentComm()
	comm2 := actionItem.GetNewComm()

	if comm1.GetCommodityType() != comm2.GetCommodityType() {
		err := fmt.Errorf("commodity type dis.match %v Vs. %v", comm1.CommodityType.String(), comm2.CommodityType.String())
		glog.Error(err.Error())
		return err
	}

	ctype := comm2.GetCommodityType()

	//1. check capacity change
	delta := math.Abs(comm1.GetCapacity() - comm2.GetCapacity())
	if delta >= epsilon {
		amount := comm2.GetCapacity()
		if amount < smallestAmount {
			err := fmt.Errorf("commodity amount is too small %v, reset to %v", amount, smallestAmount)
			glog.Errorf(err.Error())
			amount = smallestAmount
		}

		if err := r.buildResourceList(pod, ctype, amount, spec.NewCapacity); err != nil {
			err = fmt.Errorf("Failed to build resource list when ResizeCapacity %v %v: %v", ctype.String(), amount, err)
			glog.Error(err.Error())
			return err
		}
		glog.V(3).Infof("Resize pod-%v %v Capacity to %v", pod.Name, ctype.String(), amount)
	}

	//2. check reservation
	delta = math.Abs(comm1.GetReservation() - comm2.GetReservation())
	if delta >= epsilon {
		amount := comm2.GetReservation()
		if amount < smallestAmount {
			err := fmt.Errorf("commodity amount is too small %v, reset to %v", amount, smallestAmount)
			glog.Errorf(err.Error())
			amount = smallestAmount
		}

		if err := r.buildResourceList(pod, ctype, amount, spec.NewRequest); err != nil {
			err = fmt.Errorf("Failed to build resource list when ResizeReservation %v %v: %v", ctype.String(), amount, err)
			glog.Error(err.Error())
			return err
		}
		glog.V(3).Infof("Resize pod-%v %v Reservation to %v", pod.Name, ctype.String(), amount)
	}

	return nil
}

// fix-OM-30019: when resizing Capacity, if request is not specified, then the request will be equal to the new capacity.
// Solution: if request is not specified, then set it to zero.
func (r *ContainerResizer) setZeroRequest(pod *k8sapi.Pod, containerIdx int, spec *containerResizeSpec) error {
	//1. check whether Capacity resizing is involved.
	if len(spec.NewCapacity) < 1 {
		glog.V(3).Infof("No capacity resizing, no need to set request.")
		return nil
	}

	//2. for each type of resource Capacity resizing, check whether the request is specified
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
			glog.V(3).Infof("set unspecified %v request to zero", k)
		}
	}

	return nil
}

func (r *ContainerResizer) buildResizeAction(actionItem *proto.ActionItemDTO, pod *k8sapi.Pod) (*containerResizeSpec, error) {

	//1. get hosting Pod and containerIndex
	entity := actionItem.GetTargetSE()
	containerId := entity.GetId()

	_, containerIndex, err := idutil.ParseContainerId(containerId)
	if err != nil {
		glog.Errorf("failed to parse podId to build resizeAction: %v", err)
		return nil, err
	}

	if containerIndex < 0 || containerIndex > len(pod.Spec.Containers) {
		err = fmt.Errorf("Invalidate containerIndex %d", containerIndex)
		glog.Error(err.Error())
		return nil, err
	}

	//2. build the new resource Capacity
	resizeSpec := NewContainerResizeSpec(containerIndex)
	if err = r.buildResourceLists(pod, actionItem, resizeSpec); err != nil {
		glog.Errorf("failed to build NewCapacity to build resizeAction: %v", err)
		return nil, err
	}

	if err = r.setZeroRequest(pod, containerIndex, resizeSpec); err != nil {
		glog.Errorf("failed to adjust request.")
		return nil, err
	}

	return resizeSpec, nil
}

// Execute executes the container resize action
// The error info will be shown in UI
func (r *ContainerResizer) Execute(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	actionItem := input.ActionItem
	pod := input.Pod

	spec, err := r.buildResizeAction(actionItem, pod)
	if err != nil {
		return &TurboActionExecutorOutput{}, err
	}

	//2. execute the Action
	npod, err := r.executeAction(spec, pod)
	if err != nil {
		return &TurboActionExecutorOutput{}, err
	}

	return &TurboActionExecutorOutput{
		Succeeded: true,
		OldPod:    pod,
		NewPod:    npod,
	}, nil
}

func (r *ContainerResizer) executeAction(resizeSpec *containerResizeSpec, pod *k8sapi.Pod) (*k8sapi.Pod, error) {
	//1. check
	if err := r.preActionCheck(resizeSpec, pod); err != nil {
		glog.Errorf("Resize action aborted: %v.", err)
		return nil, err
	}

	//2. get parent controller
	fullName := util.BuildIdentifier(pod.Namespace, pod.Name)
	parentKind, parentName, err := podutil.GetPodParentInfo(pod)
	if err != nil {
		glog.Errorf("Resize action failed: failed to get pod[%s] parent info: %v.", fullName, err)
		return nil, err
	}

	if !util.SupportedParent(parentKind) {
		err = fmt.Errorf("Parent kind(%v) is not supported", parentKind)
		glog.Errorf("Resize action aborted: %v.", err)
		return nil, err
	}

	//3. resize
	id := fmt.Sprintf("%s/%s-%d", pod.Namespace, pod.Name, resizeSpec.Index)
	if parentKind == "" {
		glog.V(2).Infof("Begin to resize barepod container[%s].", id)
	} else {
		glog.V(2).Infof("Begin to resize controller container[%s] parent=%s/%s.", id, parentKind, parentName)
	}

	return resizeContainer(r.kubeClient, pod, resizeSpec, defaultRetryMore)
}

// Check whether the action should be executed.
func (r *ContainerResizer) preActionCheck(resizeSpec *containerResizeSpec, pod *k8sapi.Pod) error {
	fullName := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)

	// Check if the pod privilege is supported
	if !util.SupportPrivilegePod(pod, r.sccAllowedSet) {
		err := fmt.Errorf("Pod %s has unsupported SCC", fullName)
		return err
	}

	// Check if the resize spec is empty
	if len(resizeSpec.NewCapacity) < 1 && len(resizeSpec.NewRequest) < 1 {
		err := fmt.Errorf("Resize specification is empty")
		return err
	}

	return nil
}
