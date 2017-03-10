package main

import (
	"runtime"

	"k8s.io/kubernetes/pkg/util/flag"
	"k8s.io/kubernetes/pkg/util/logs"
	"k8s.io/kubernetes/pkg/version/verflag"
	_ "k8s.io/kubernetes/plugin/pkg/scheduler/algorithmprovider"

	"github.com/vmturbo/kubeturbo/cmd/kube-vmtactionsimulator/builder"
	vmtaction "github.com/vmturbo/kubeturbo/pkg/action"
	turboscheduler "github.com/vmturbo/kubeturbo/pkg/scheduler"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
)

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	builder := builder.NewSimulatorBuilder()
	builder.AddFlags(pflag.CommandLine)

	flag.InitFlags()
	logs.InitLogs()
	defer logs.FlushLogs()

	verflag.PrintAndExitIfRequested()

	builder.Init(pflag.CommandLine.Args())
	simulator, err := builder.Build()
	if err != nil {
		glog.Errorf("error getting simulator: %s", err)
		return
	}

	action := simulator.Action()
	namespace := simulator.Namespace()

	actionHandlerConfig := vmtaction.NewActionHandlerConfig(simulator.KubeClient())
	turboSched := turboscheduler.NewTurboScheduler(simulator.KubeClient(), "", "", "")
	actionHandler := vmtaction.NewActionHandler(actionHandlerConfig, turboSched)

	if action == "move" || action == "Move " {
		podName := simulator.Pod()
		destinationNode := simulator.Destination()
		podIdentifier := namespace + ":" + podName

		targetSE, _ := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_CONTAINER_POD, podIdentifier).Create()

		var ips []string
		ips = append(ips, destinationNode)
		vmData := &proto.EntityDTO_VirtualMachineData{
			IpAddress: ips,
		}
		newSE, _ := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_VIRTUAL_MACHINE, destinationNode).
			VirtualMachineData(vmData).
			Create()

		actionType := proto.ActionItemDTO_MOVE
		actionItemDTO := &proto.ActionItemDTO{
			ActionType: &actionType,
			TargetSE:   targetSE,
			NewSE:      newSE,
		}

		actionHandler.Execute(actionItemDTO, -1)
		return
	} else if action == "provision" {
		podName := simulator.Pod()
		podIdentifier := namespace + ":" + podName

		targetSE, _ := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_CONTAINER_POD, podIdentifier).Create()
		newSE := targetSE

		actionType := proto.ActionItemDTO_PROVISION
		actionItemDTO := &proto.ActionItemDTO{
			ActionType: &actionType,
			TargetSE:   targetSE,
			NewSE:      newSE,
		}

		actionHandler.Execute(actionItemDTO, -1)
		return
	} else if action == "unbind" {
		app := simulator.Application()
		vApp := simulator.VirtualApplication()

		targetSE, _ := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_VIRTUAL_APPLICATION, vApp).Create()
		currentSE, _ := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_APPLICATION, app).Create()

		actionType := proto.ActionItemDTO_MOVE
		actionItemDTO := &proto.ActionItemDTO{
			ActionType: &actionType,
			TargetSE:   targetSE,
			CurrentSE:  currentSE,
		}
		actionHandler.Execute(actionItemDTO, -1)
		return
	}
}
