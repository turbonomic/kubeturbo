package executor

import (
	//"errors"
	//"fmt"
	//
	//client "k8s.io/kubernetes/pkg/client/unversioned"
	//
	"github.com/vmturbo/kubeturbo/pkg/action/turboaction"
	//turboscheduler "github.com/vmturbo/kubeturbo/pkg/scheduler"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	//"github.com/golang/glog"
	//"github.com/vmturbo/kubeturbo/pkg/turbostore"
)

type TurboActionExecutor interface {
	Execute(actionItem *proto.ActionItemDTO) (*turboaction.TurboAction, error)
}
//
//// VMTActionExecutor is responsible for executing different kinds of actions requested by vmt server.
//type ActionExecutor struct {
//	kubeClient         *client.Client
//	executedActionChan chan *turboaction.TurboAction
//
//	reScheduler      *ReScheduler
//	horizontalScaler *HorizontalScaler
//}
//
//// Create new VMT Actor. Must specify the kubernetes client.
//func NewVMTActionExecutor(client *client.Client, broker turbostore.Broker, scheduler *turboscheduler.TurboScheduler,
//	executedActionChan chan *turboaction.TurboAction) *ActionExecutor {
//
//	rescheduler := NewReScheduler(client, broker)
//	horizontalScaler := NewHorizontalScaler(client, broker, scheduler)
//
//	return &ActionExecutor{
//		kubeClient:         client,
//		executedActionChan: executedActionChan,
//
//		reScheduler:      rescheduler,
//		horizontalScaler: horizontalScaler,
//	}
//}
//
//// Switch between different types of the actions. Then call the actually corresponding execution method.
//func (e *ActionExecutor) ExecuteAction(actionItem *proto.ActionItemDTO) error {
//	if actionItem == nil {
//		return nil, errors.New("ActionItem received in is null")
//	}
//	glog.V(3).Infof("Receive a %s action request.", actionItem.GetActionType())
//
//	var action *turboaction.TurboAction
//	var err error
//	switch actionItem.GetActionType() {
//	case proto.ActionItemDTO_MOVE:
//		// Here we must make sure the TargetSE is a Pod and NewSE is either a VirtualMachine or a PhysicalMachine.
//		if actionItem.GetTargetSE().GetEntityType() == proto.EntityDTO_CONTAINER_POD {
//			// A regular MOVE action
//			glog.V(2).Infof("Now execute a move action.")
//			action, err = e.reScheduler.Execute(actionItem)
//		} else if actionItem.GetTargetSE().GetEntityType() == proto.EntityDTO_VIRTUAL_APPLICATION {
//			// An UnBind action
//			glog.V(2).Infof("Now execute a unbind action.")
//			action, err = e.horizontalScaler.Execute(actionItem)
//		} else {
//			// NOT Supported
//			return fmt.Errorf("The service entity to be moved is not a Pod. Got %s", actionItem.GetTargetSE().GetEntityType())
//		}
//		break
//	case proto.ActionItemDTO_PROVISION:
//		// A Provision action
//		glog.V(2).Infof("Now execute a provision action.")
//		action, err = e.horizontalScaler.Execute(actionItem)
//		break
//	default:
//		return fmt.Errorf("Action %s not supported", actionItem.GetActionType())
//
//	}
//
//	if err != nil {
//		return nil, err
//	}
//	e.executedActionChan <- action
//	return nil
//}
