package action

import (
	"fmt"

	client "k8s.io/kubernetes/pkg/client/unversioned"

	"github.com/vmturbo/kubeturbo/pkg/registry"
	"github.com/vmturbo/kubeturbo/pkg/storage"

	"github.com/vmturbo/vmturbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

// VMTActionExecutor is responsilbe for executing different kinds of actions requested by vmt server.
type VMTActionExecutor struct {
	KubeClient  *client.Client
	EtcdStorage storage.Storage

	rescheduler      *Rescheduler
	horizontalScaler *HorizontalScaler
}

// Create new VMT Actor. Must specify the kubernetes client.
func NewVMTActionExecutor(client *client.Client, etcdStorage storage.Storage) *VMTActionExecutor {

	vmtEventRegistry := registry.NewVMTEventRegistry(etcdStorage)

	rescheduler := NewRescheduler(client, vmtEventRegistry)
	horizontalScaler := NewHorizontalScaler(client, vmtEventRegistry)

	return &VMTActionExecutor{
		KubeClient:  client,
		EtcdStorage: etcdStorage,

		rescheduler:      rescheduler,
		horizontalScaler: horizontalScaler,
	}
}

// Switch between different types of the actions. Then call the actually corresponding execution method.
func (this *VMTActionExecutor) ExcuteAction(actionItem *proto.ActionItemDTO, msgID int32) (*registry.VMTEvent, error) {
	if actionItem == nil {
		return nil, fmt.Errorf("ActionItem received in is null")
	}
	glog.V(3).Infof("Receive a %s action request.", actionItem.GetActionType())

	if actionItem.GetActionType() == proto.ActionItemDTO_MOVE {
		// Here we must make sure the TargetSE is a Pod and NewSE is either a VirtualMachine or a PhysicalMachine.
		if actionItem.GetTargetSE().GetEntityType() == proto.EntityDTO_CONTAINER_POD {
			// A regular MOVE
			glog.V(4).Infof("Now moving pod")
			return this.rescheduler.MovePod(actionItem, msgID)
		} else if actionItem.GetTargetSE().GetEntityType() == proto.EntityDTO_VIRTUAL_APPLICATION {
			// An UnBind Action
			return this.horizontalScaler.ScaleIn(actionItem, msgID)
		} else {
			// NOT Supported
			return nil, fmt.Errorf("The service entity to be moved is not a Pod. Got %s", actionItem.GetTargetSE().GetEntityType())
		}
	} else if actionItem.GetActionType() == proto.ActionItemDTO_PROVISION {
		glog.V(4).Infof("Now Provision Pods")
		return this.horizontalScaler.ScaleOut(actionItem, msgID)
	} else {
		return nil, fmt.Errorf("Action %s not supported", actionItem.GetActionType())
	}
}
