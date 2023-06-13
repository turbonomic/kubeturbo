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

	//1. Get replica diff
	diff, err := getReplicaDiff(actionItem)
	if err != nil {
		glog.Errorf("Failed to get replica diff: %v", err)
		return nil, err
	}
	//2. Prepare controllerUpdater
	namespace, controllerName, kind, err := getControllerInfo(actionItem.GetTargetSE())
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
