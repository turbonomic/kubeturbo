package executor

import (
	"fmt"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type MachineActionExecutor struct {
	TurboK8sActionExecutor
}

func NewMachineActionExecutor(ae TurboK8sActionExecutor) *MachineActionExecutor {
	return &MachineActionExecutor{
		ae,
	}
}

// Execute : executes the scale action.
func (s *MachineActionExecutor) Execute(vmDTO *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	nodeName := vmDTO.ActionItem.GetCurrentSE().GetDisplayName()
	var actionType ActionType
	switch vmDTO.ActionItem.GetActionType() {
	case proto.ActionItemDTO_PROVISION:
		actionType = ProvisionAction
		break
	case proto.ActionItemDTO_SUSPEND:
		actionType = SuspendAction
		break
	default:
		return nil, fmt.Errorf("Unsupported action type %v", vmDTO.ActionItem.GetActionType())
	}
	// Get on with it.
	controller, err := newController(nodeName, 1, actionType, s.cApiClient, s.kubeClient)
	if err != nil {
		return nil, err
	}
	err = controller.checkPreconditions()
	if err != nil {
		return nil, err
	}
	err = controller.executeAction()
	if err != nil {
		return nil, err
	}
	err = controller.checkSuccess()
	if err != nil {
		return nil, err
	}
	return &TurboActionExecutorOutput{Succeeded: true}, nil
}
