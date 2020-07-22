package executor

import (
	"fmt"

	"github.com/golang/glog"

	"github.com/turbonomic/kubeturbo/pkg/action/util"
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
	actionItem := input.ActionItems[0]
	pod := input.Pod

	//1. Get replica diff
	diff, err := getReplicaDiff(actionItem)
	if err != nil {
		glog.Errorf("Failed to get replica diff: %v", err)
		return nil, err
	}
	//2. Prepare controllerUpdater
	controllerUpdater, err := newK8sControllerUpdaterViaPod(h.kubeClient, h.dynamicClient, pod, h.ormClient)
	if err != nil {
		glog.Errorf("Failed to create controllerUpdater: %v", err)
		return &TurboActionExecutorOutput{}, err
	}
	//3. Execute the action to update replica diff of the controller
	err = controllerUpdater.updateWithRetry(&controllerSpec{replicasDiff: diff})
	if err != nil {
		glog.Errorf("Failed to scale %s: %v", pod.Name, err)
		return &TurboActionExecutorOutput{}, err
	}
	podFullName := util.BuildIdentifier(pod.Namespace, pod.Name)
	glog.V(2).Infof("Action HorizontalScale for pod[%v] succeeded.", podFullName)
	return &TurboActionExecutorOutput{Succeeded: true}, nil
}

func getReplicaDiff(action *proto.ActionItemDTO) (int32, error) {
	atype := action.GetActionType()
	if atype == proto.ActionItemDTO_PROVISION {
		// Scale out, increase the replica. diff = 1.
		return 1, nil
	} else if atype == proto.ActionItemDTO_SUSPEND {
		// Scale in, decrease the replica. diff = -1.
		return -1, nil
	} else {
		return 0, fmt.Errorf("action %v is not a scaling action", atype.String())
	}
}
