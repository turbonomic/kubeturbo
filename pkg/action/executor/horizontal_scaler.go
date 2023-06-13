package executor

import (
	"fmt"
	"strconv"

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

	//1. Get replica diff
	diff, err := getReplicaDiff(actionItem)
	if err != nil {
		glog.Errorf("Failed to get replica diff: %v", err)
		return nil, err
	}
	//2. Prepare controllerUpdater
	namespace, controllerName, kind, err := getWorkloadControllerInfo(actionItem.GetTargetSE())
	if err != nil {
		glog.Errorf("Failed to get controller information: %v", err)
		return &TurboActionExecutorOutput{}, err
	}
	controllerUpdater, err := newK8sControllerUpdater(h.clusterScraper, h.ormClient, kind, controllerName,
		"", namespace, h.k8sClusterId, nil, h.gitConfig)
	if err != nil {
		glog.Errorf("Failed to create controllerUpdater: %v", err)
		return &TurboActionExecutorOutput{}, err
	}
	//3. Execute the action to update replica diff of the controller
	controllerFullName := util.BuildIdentifier(namespace, controllerName)
	err = controllerUpdater.updateWithRetry(&controllerSpec{replicasDiff: diff})
	if err != nil {
		glog.Errorf("Failed to scale %s: %v", controllerFullName, err)
		return &TurboActionExecutorOutput{}, err
	}
	glog.V(2).Infof("Action HorizontalScale for workload controller[%v] succeeded.", controllerFullName)
	return &TurboActionExecutorOutput{Succeeded: true}, nil
}

func getReplicaDiff(action *proto.ActionItemDTO) (int32, error) {
	atype := action.GetActionType()
	if atype != proto.ActionItemDTO_HORIZONTAL_SCALE {
		return 0, fmt.Errorf("action %v is not a horizontal scaling action", atype.String())
	}

	data := action.GetContextData()
	var oldValue, newValue string

	for _, item := range data {
		switch *item.ContextKey {
		case "OLD_REPLICAS":
			oldValue = *item.ContextValue
		case "NEW_REPLICAS":
			newValue = *item.ContextValue
		}
	}

	if oldValue == "" {
		return 0, fmt.Errorf("key 'OLD_REPLICAS' not found in context data")
	}

	if newValue == "" {
		return 0, fmt.Errorf("key 'NEW_REPLICAS' not found in context data")
	}

	oldInt, err := strconv.Atoi(oldValue)
	if err != nil || oldInt <= 0 {
		return 0, fmt.Errorf("value of 'OLD_REPLICAS' must be a positive integer")
	}

	newInt, err := strconv.Atoi(newValue)
	if err != nil || newInt <= 0 {
		return 0, fmt.Errorf("value of 'NEW_REPLICAS' must be a positive integer")
	}

	return int32(newInt - oldInt), nil
}
