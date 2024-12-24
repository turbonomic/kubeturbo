package executor

import (
	"fmt"
	"math"

	"github.com/golang/glog"

	k8sapi "k8s.io/api/core/v1"

	"github.ibm.com/turbonomic/kubeturbo/pkg/action/util"
	idutil "github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/kubeclient"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	epsilon        float64 = 0.001
	smallestAmount float64 = 1.0
)

var (
	resourceCommodities = map[proto.CommodityDTO_CommodityType]struct{}{
		proto.CommodityDTO_VCPU: {},
		proto.CommodityDTO_VMEM: {},
	}

	resourceRequestCommodities = map[proto.CommodityDTO_CommodityType]struct{}{
		proto.CommodityDTO_VCPU_REQUEST: {},
		proto.CommodityDTO_VMEM_REQUEST: {},
	}
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
	sccAllowedSet map[string]struct{},
) *ContainerResizer {
	return &ContainerResizer{
		TurboK8sActionExecutor: ae,
		kubeletClient:          kubeletClient,
		sccAllowedSet:          sccAllowedSet,
	}
}

func (r *ContainerResizer) buildResourceList(cType proto.CommodityDTO_CommodityType,
	amount float64, result k8sapi.ResourceList,
) error {
	switch cType {
	case proto.CommodityDTO_VCPU, proto.CommodityDTO_VCPU_REQUEST:
		cpu, err := genCPUQuantity(amount)
		if err != nil {
			return fmt.Errorf("failed to build cpu.Capacity: %v", err)
		}
		result[k8sapi.ResourceCPU] = cpu
	case proto.CommodityDTO_VMEM, proto.CommodityDTO_VMEM_REQUEST:
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
func (r *ContainerResizer) buildResourceLists(resizerName string, actionItem *proto.ActionItemDTO,
	spec *containerResizeSpec,
) error {
	comm1 := actionItem.GetCurrentComm()
	comm2 := actionItem.GetNewComm()

	if comm1.GetCommodityType() != comm2.GetCommodityType() {
		return fmt.Errorf("commodity type does not match %v vs %v",
			comm1.CommodityType.String(), comm2.CommodityType.String())
	}

	cType := comm2.GetCommodityType()

	// 1. check capacity change
	change, amount := getNewAmount(comm1.GetCapacity(), comm2.GetCapacity())
	if change {
		var resourceList k8sapi.ResourceList
		if _, exists := resourceCommodities[cType]; exists {
			resourceList = spec.NewCapacity
		} else if _, exists := resourceRequestCommodities[cType]; exists {
			resourceList = spec.NewRequest
		} else {
			return fmt.Errorf("failed to build resource list when resize %s capacity to %v: %s commodity type is not supported",
				cType, amount, cType)
		}
		if err := r.buildResourceList(cType, amount, resourceList); err != nil {
			return fmt.Errorf("failed to build resource list when resize %s capacity to %v: %v",
				cType, amount, err)
		}
		glog.V(3).Infof("Resize %s %s Capacity to %v", resizerName, cType, amount)
	}
	return nil
}

func (r *ContainerResizer) buildResizeSpec(actionItem *proto.ActionItemDTO, resizerName string, podSpec *k8sapi.PodSpec, containerIndex int) (*containerResizeSpec, error) {
	if containerIndex < 0 || containerIndex > len(podSpec.Containers) {
		return nil, fmt.Errorf("cannot find the container <%v> with the index <%v> in the parents pod spec", actionItem.GetCurrentSE().GetDisplayName(), containerIndex)
	}

	// build the new resource requirements
	resizeSpec := NewContainerResizeSpec(containerIndex)
	if err := r.buildResourceLists(resizerName, actionItem, resizeSpec); err != nil {
		return nil, fmt.Errorf("failed to build resizeSpec: %v", err)
	}

	// check if the resize spec is empty
	if len(resizeSpec.NewCapacity) < 1 && len(resizeSpec.NewRequest) < 1 {
		return nil, fmt.Errorf("resize specification is empty")
	}

	return resizeSpec, nil
}

// Execute executes the container resize action
// The error info will be shown in UI
func (r *ContainerResizer) Execute(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	actionItem := input.ActionItems[0]
	pod := input.Pod

	// check if the pod privilege is supported
	supported, err := util.SupportPrivilegePod(pod, r.sccAllowedSet)
	if !supported {
		glog.Errorf("Failed to execute resize action: %v", err)
		return nil, err
	}

	// get hosting Pod and containerIndex
	entity := actionItem.GetTargetSE()
	containerId := entity.GetId()

	_, containerIndex, err := idutil.ParseContainerId(containerId)
	if err != nil {
		return nil, fmt.Errorf("failed to parse container index to build resizeAction: %v", err)
	}

	// build resize specification
	spec, err := r.buildResizeSpec(actionItem, pod.Name, &pod.Spec, containerIndex)
	if err != nil {
		glog.Errorf("Failed to execute resize action: %v", err)
		return nil, err
	}

	// execute the Action
	npod, err := resizeContainer(
		r.clusterScraper,
		pod,
		spec,
		actionItem.GetConsistentScalingCompliance(),
		r.ormClient,
		r.gitConfig,
		r.k8sClusterId,
	)
	if err != nil {
		glog.Errorf("Failed to execute resize action: %v", err)
		return nil, err
	}

	return &TurboActionExecutorOutput{
		Succeeded: true,
		OldPod:    pod,
		NewPod:    npod,
	}, nil
}

func (r *ContainerResizer) ExecuteList(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	return nil, fmt.Errorf("batched action execution is not supported for container resize")
}
