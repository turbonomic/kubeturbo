package main

import (
	"runtime"

	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/pkg/version/verflag"

	"github.com/vmturbo/kubeturbo/cmd/kube-vmtactionsimulator/builder"
	vmtaction "github.com/vmturbo/kubeturbo/pkg/action"

	"github.com/vmturbo/vmturbo-go-sdk/sdk"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	builder := builder.NewSimulatorBuilder()
	builder.AddFlags(pflag.CommandLine)

	util.InitFlags()
	util.InitLogs()
	defer util.FlushLogs()

	verflag.PrintAndExitIfRequested()

	builder.Init(pflag.CommandLine.Args())
	simulator, err := builder.Build()
	if err != nil {
		glog.Errorf("error getting simulator: %s", err)
		return
	}

	action := simulator.Action()
	namespace := simulator.Namespace()

	// The simulator can simulate move, get and provision action now.
	actor := vmtaction.NewVMTActionExecutor(simulator.KubeClient(), simulator.Etcd())
	if action == "move" || action == "Move " {
		podName := simulator.Pod()
		destinationNode := simulator.Destination()
		podIdentifier := namespace + "/" + podName

		targetSE := sdk.NewEntityDTOBuilder(sdk.EntityDTO_CONTAINER_POD, podIdentifier).Create()
		newSE := sdk.NewEntityDTOBuilder(sdk.EntityDTO_VIRTUAL_MACHINE, destinationNode).Create()
		var ips []string
		ips = append(ips, destinationNode)
		vmData := &sdk.EntityDTO_VirtualMachineData{
			IpAddress: ips,
		}
		newSE.VirtualMachineData = vmData

		actionType := sdk.ActionItemDTO_MOVE
		actionItemDTO := &sdk.ActionItemDTO{
			ActionType: &actionType,
			TargetSE:   targetSE,
			NewSE:      newSE,
		}

		err := actor.ExcuteAction(actionItemDTO, -1)
		if err != nil {
			glog.Errorf("Error executing move: %v", err)
		}
		return
	} else if action == "provision" {
		podName := simulator.Pod()
		podIdentifier := namespace + "/" + podName

		targetSE := sdk.NewEntityDTOBuilder(sdk.EntityDTO_CONTAINER_POD, podIdentifier).Create()
		newSE := targetSE

		actionType := sdk.ActionItemDTO_PROVISION
		actionItemDTO := &sdk.ActionItemDTO{
			ActionType: &actionType,
			TargetSE:   targetSE,
			NewSE:      newSE,
		}

		err := actor.ExcuteAction(actionItemDTO, -1)
		if err != nil {
			glog.Errorf("Error executing provision: %v", err)
		}
		return
	} else if action == "unbind" {
		app := simulator.Application()
		vApp := simulator.VirtualApplication()

		targetSE := sdk.NewEntityDTOBuilder(sdk.EntityDTO_VIRTUAL_APPLICATION, vApp).Create()
		currentSE := sdk.NewEntityDTOBuilder(sdk.EntityDTO_APPLICATION, app).Create()

		actionType := sdk.ActionItemDTO_MOVE
		actionItemDTO := &sdk.ActionItemDTO{
			ActionType: &actionType,
			TargetSE:   targetSE,
			CurrentSE:  currentSE,
		}
		err := actor.ExcuteAction(actionItemDTO, -1)
		if err != nil {
			glog.Errorf("Error executing provision: %v", err)
		}
		return
	}
}
