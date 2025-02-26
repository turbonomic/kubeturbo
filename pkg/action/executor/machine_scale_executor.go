package executor

import (
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/golang/glog"
	actionutil "github.ibm.com/turbonomic/kubeturbo/pkg/action/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/turbostore"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type MachineScalerType string

const (
	MachineScalerTypeCAPI MachineScalerType = "TypeCAPI" // Cluster API based scaler
	MachineScalerTypeCP   MachineScalerType = "TypeCP"   // Cloud provider based scaler
	actionResultSuccess                     = "Success"
)

type MachineActionExecutor struct {
	executor      TurboK8sActionExecutor
	cache         *turbostore.Cache
	cAPINamespace string
	lockMap       *actionutil.ExpirationMap
}

func NewMachineActionExecutor(namespace string, ae TurboK8sActionExecutor, lockMap *actionutil.ExpirationMap) *MachineActionExecutor {
	return &MachineActionExecutor{
		executor:      ae,
		cache:         turbostore.NewCache(),
		cAPINamespace: namespace,
		lockMap:       lockMap,
	}
}

func (s *MachineActionExecutor) unlock(key string) {
	err := s.cache.Delete(key)
	if err != nil {
		glog.Errorf("Error unlocking action %v", err)
	}
}

// Execute : executes the scale action.
func (s *MachineActionExecutor) Execute(vmDTO *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	actionItem := vmDTO.ActionItems[0]
	nodeName := actionItem.GetTargetSE().GetDisplayName()
	var diff int32
	switch actionItem.GetActionType() {
	case proto.ActionItemDTO_PROVISION:
		diff = 1
	case proto.ActionItemDTO_SUSPEND:
		diff = -1
	default:
		return nil, fmt.Errorf("unsupported action type %v", vmDTO.ActionItems[0].GetActionType())
	}

	controller, err := s.createSingleNodeController(nodeName, diff, actionutil.GetActionItemId(actionItem))
	if err != nil {
		return nil, err
	}
	scaleDirection := "up"
	scaleAmount := diff
	if diff < 0 {
		scaleDirection = "down"
		scaleAmount = -diff
	}
	key := controller.controllerName
	glog.V(2).Infof("Starting to scale %s the machineSet %s by %d replica", scaleDirection, controller.controllerName, scaleAmount)
	// See if we already have this.
	_, ok := s.cache.Get(key)
	if ok {
		return nil, fmt.Errorf("the action against the %s is already running", key)
	}
	s.cache.Add(key, &key)
	defer s.unlock(key)

	err = controller.executeAction()
	if err != nil {
		return nil, err
	}
	glog.V(2).Infof("Completed scaling %s the machineSet %s by %d replica", scaleDirection, key, scaleAmount)

	return &TurboActionExecutorOutput{Succeeded: true}, nil
}

// ExecuteList marks multiple nodes for deletion within the same machineset
func (s *MachineActionExecutor) ExecuteList(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	actionItemIdNodeName := map[int64]string{}

	for _, actionItem := range input.ActionItems {
		nodeName := actionItem.GetTargetSE().GetDisplayName()
		glog.V(3).Infof("Received suspension [actionId=%s] for node %s", actionItem.GetUuid(), nodeName)
		actionItemIdNodeName[actionutil.GetActionItemId(actionItem)] = nodeName
	}

	glog.V(4).Infof("Create controller for nodes: %v", actionItemIdNodeName)

	// All node suspensions are grouped together
	controllers, err := s.createMultiNodeController(&actionItemIdNodeName)
	if err != nil {
		return nil, err
	}
	glog.V(4).Infof("Created %d controllers for action execution", len(controllers))
	eg := new(errgroup.Group)
	for _, controller := range controllers {
		machinesetController := controller
		key := machinesetController.controllerName
		_, ok := s.cache.Get(key)
		if ok {
			err = fmt.Errorf("the action against the %s is already running", key)
			for _, machineInfo := range machinesetController.machinesInfo {
				if machineInfo.actionResult == actionResultSuccess {
					machineInfo.actionResult = err.Error()
				}
			}
			continue
		}
		s.cache.Add(key, &key)
		defer s.unlock(key)
		glog.V(4).Infof("Start suspend nodes for controller %s", machinesetController.controllerName)
		eg.Go(func() error {
			return machinesetController.executeAction()
		})
	}
	err = eg.Wait()
	actionItemStatuses := map[int64]string{}
	for _, controller := range controllers {
		for _, machineInfo := range controller.machinesInfo {
			actionItemStatuses[machineInfo.actionItemId] = machineInfo.actionResult
		}
	}
	return &TurboActionExecutorOutput{ActionItemsStatuses: actionItemStatuses}, err
}
