package executor

import (
	"fmt"
	"github.com/golang/glog"
	"time"

	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/action/util"
	podutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	goutil "github.com/turbonomic/kubeturbo/pkg/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type HorizontalScaler struct {
	TurboK8sActionExecutor
}

func NewHorizontalScaler(ae TurboK8sActionExecutor) *HorizontalScaler {
	return &HorizontalScaler{
		TurboK8sActionExecutor: ae,
	}
}

func (h *HorizontalScaler) Execute(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	actionItem := input.ActionItem
	pod := input.Pod
	podFullName := util.BuildIdentifier(pod.Namespace, pod.Name)

	//1. prepare
	helper, err := h.prepareHelper(actionItem, pod)
	if err != nil {
		glog.Errorf("Failed to prepare action:%v, abort action %++v", err, actionItem)
		return &TurboActionExecutorOutput{}, fmt.Errorf("Aborted")
	}

	//2. execute the action
	if err = h.do(helper); err != nil {
		glog.Errorf("Failed to execute action: %v, abort action %++v", err, actionItem)
		return &TurboActionExecutorOutput{}, fmt.Errorf("Failed")
	}

	//3. check action result
	glog.V(2).Infof("Begin to check action resulf of HorizontalScale for pod[%v]", podFullName)
	if err = h.checkResult(helper); err != nil {
		glog.Errorf("HorizontalScale checking failed: %v", err)
		return &TurboActionExecutorOutput{}, fmt.Errorf("Failed")
	}
	glog.V(2).Infof("Action HorizontalScale for pod[%v] succeeded.", podFullName)

	return &TurboActionExecutorOutput{Succeeded: true}, nil
}

func (h *HorizontalScaler) preActionCheck(action *proto.ActionItemDTO) error {
	return nil
}

func (h *HorizontalScaler) prepareHelper(action *proto.ActionItemDTO, pod *api.Pod) (*scaleHelper, error) {
	//1. get helper
	helper, _ := NewScaleHelper(h.kubeClient, pod.Namespace, pod.Name)

	//2. get replica diff
	diff, err := h.getReplicaDiff(action)
	if err != nil {
		glog.Errorf("Failed to get ReplicaDiff: %v", err)
		return nil, err
	}
	helper.diff = diff

	//3. find parent info
	parentKind, parentName, err := podutil.GetPodGrandInfo(h.kubeClient, pod)
	if err != nil {
		glog.Errorf("Failed to get parent info for pod: %s/%s", pod.Namespace, pod.Name)
		return nil, err
	}
	if err = helper.SetParent(parentKind, parentName); err != nil {
		return nil, err
	}

	return helper, nil
}

func (h *HorizontalScaler) getReplicaDiff(action *proto.ActionItemDTO) (int32, error) {
	atype := action.GetActionType()
	if atype == proto.ActionItemDTO_PROVISION {
		// Scale out, increase the replica. diff = 1.
		return 1, nil
	} else if atype == proto.ActionItemDTO_MOVE {
		// TODO, unbind action is send as MOVE. This requires server side change.
		// Scale in, decrease the replica. diff = -1.
		return -1, nil
	} else {
		err := fmt.Errorf("Action[%v] is not a scaling action.", atype.String())
		glog.Errorf(err.Error())
		return 0, err
	}
}

func (h *HorizontalScaler) do(helper *scaleHelper) error {
	fullName := fmt.Sprintf("%s-%s/%s", helper.kind, helper.nameSpace, helper.controllerName)

	// update replica number
	retryNum := defaultRetryLess
	interval := defaultUpdateReplicaSleep
	timeout := time.Duration(retryNum+1) * interval
	err := goutil.RetryDuring(retryNum, timeout, interval, func() error {
		inerr := helper.updateReplicaNum(h.kubeClient, helper.nameSpace, helper.controllerName, helper.diff)
		if inerr != nil {
			glog.Errorf("[%s] failed to update replica num: %v", fullName, inerr)
			return fmt.Errorf("Failed")
		}
		return inerr
	})

	return err
}

func (h *HorizontalScaler) checkResult(helper *scaleHelper) error {
	// do nothing
	return nil
}
