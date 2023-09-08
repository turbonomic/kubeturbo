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
	actionType := actionItem.GetActionType()
	pod := input.Pod

	//1. Get replica diff
	diff, err := getReplicaDiff(actionItem)
	if err != nil {
		glog.Errorf("Failed to get replica diff: %v", err)
		return nil, err
	}
	//2. Prepare controllerUpdater
	// for SLO pod provision and suspension
	var controllerUpdater *k8sControllerUpdater
	var updaterErr error
	var targetFullName string
	if pod != nil && actionType != proto.ActionItemDTO_HORIZONTAL_SCALE {
		targetFullName = util.BuildIdentifier(pod.Namespace, pod.Name)
		controllerUpdater, updaterErr = newK8sControllerUpdaterViaPod(h.clusterScraper,
			pod, h.ormClient, h.gitConfig, h.k8sClusterId, proto.ActionItemDTO_HORIZONTAL_SCALE)
	} else {
		namespace, controllerName, kind, err := getWorkloadControllerInfo(actionItem.GetTargetSE())
		if err != nil {
			glog.Errorf("Failed to get controller information: %v", err)
			return &TurboActionExecutorOutput{}, err
		}
		targetFullName = util.BuildIdentifier(namespace, controllerName)
		controllerUpdater, updaterErr = newK8sControllerUpdater(h.clusterScraper, h.ormClient, kind, controllerName,
			"", namespace, h.k8sClusterId, nil, h.gitConfig, proto.ActionItemDTO_HORIZONTAL_SCALE)
	}

	if updaterErr != nil {
		glog.Errorf("Failed to create controllerUpdater: %v", updaterErr)
		return &TurboActionExecutorOutput{}, updaterErr
	}
	//3. Execute the action to update replica diff of the controller
	err = controllerUpdater.updateWithRetry(&controllerSpec{replicasDiff: diff})
	if err != nil {
		glog.Errorf("Failed to scale %s: %v", targetFullName, err)
		return &TurboActionExecutorOutput{}, err
	}
	glog.V(2).Infof("Action HorizontalScale for workload controller[%v] succeeded.", targetFullName)
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
	}

	if atype != proto.ActionItemDTO_HORIZONTAL_SCALE {
		return 0, fmt.Errorf("action %v is not a horizontal scaling action", atype.String())
	}

	currentCom := action.GetCurrentComm()
	if currentCom == nil || currentCom.GetCommodityType() != proto.CommodityDTO_NUMBER_REPLICAS {
		return 0, fmt.Errorf("NUMBER_REPLICAS not found in currentCommodity of action DTO")
	}

	newCom := action.GetNewComm()
	if newCom == nil || newCom.GetCommodityType() != proto.CommodityDTO_NUMBER_REPLICAS {
		return 0, fmt.Errorf("NUMBER_REPLICAS not found in currentCommodity of action DTO")
	}

	oldReplicas := currentCom.GetCapacity()
	if oldReplicas < 0 {
		return 0, fmt.Errorf("value of old replicas must be a positive number")
	}

	newReplicas := newCom.GetCapacity()
	if newReplicas < 0 {
		return 0, fmt.Errorf("value of new replicas must be a positive number")
	}

	return int32(newReplicas - oldReplicas), nil
}
