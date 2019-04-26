package executor

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type MachineActionExecutor struct {
	ae    TurboK8sActionExecutor
	locks *turbostore.Cache
}

func NewMachineActionExecutor(ae TurboK8sActionExecutor) *MachineActionExecutor {
	return &MachineActionExecutor{
		ae:    ae,
		locks: turbostore.NewCache(),
	}
}

// unlock unlocks the executor to be used with the same machine set
func (s *MachineActionExecutor) unlock(lock string) {
	err := s.locks.Delete(lock)
	if err != nil {
		glog.V(4).Info("problem unlocking MachinSet " + lock)
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
		return nil, fmt.Errorf("unsupported action type %v", vmDTO.ActionItem.GetActionType())
	}
	// Get on with it.
	controller, err := newController(nodeName, 1, actionType, s.ae.cApiClient, s.ae.kubeClient)
	if err != nil {
		return nil, err
	}
	lock := controller.getLock()
	// See if we've locked already
	if _, exists := s.locks.Get(lock); exists {
		return nil, fmt.Errorf("machineSet %s is being updated already", lock)
	}
	// Locks and unlocks the executor to be used with the same machine set.
	s.locks.Add(lock, int(1))
	defer s.unlock(lock)

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
