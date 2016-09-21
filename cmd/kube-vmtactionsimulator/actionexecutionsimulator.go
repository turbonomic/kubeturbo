package main

import (
	"runtime"

	"k8s.io/kubernetes/pkg/util/flag"
	"k8s.io/kubernetes/pkg/util/logs"
	"k8s.io/kubernetes/pkg/version/verflag"

	"github.com/vmturbo/kubeturbo/cmd/kube-vmtactionsimulator/builder"
	vmtaction "github.com/vmturbo/kubeturbo/pkg/action"

	sdkbuilder "github.com/vmturbo/vmturbo-go-sdk/pkg/builder"
	"github.com/vmturbo/vmturbo-go-sdk/pkg/proto"

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

	// The simulator can simulate move, get and provision action now.
	actor := vmtaction.NewVMTActionExecutor(simulator.KubeClient(), simulator.Etcd())
	if action == "move" || action == "Move " {
		podName := simulator.Pod()
		destinationNode := simulator.Destination()
		podIdentifier := namespace + "/" + podName

		targetSE, _ := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_CONTAINER_POD, podIdentifier).Create()
		newSE, _ := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_VIRTUAL_MACHINE, destinationNode).Create()
		var ips []string
		ips = append(ips, destinationNode)
		vmData := &proto.EntityDTO_VirtualMachineData{
			IpAddress: ips,
		}
		newSE.VirtualMachineData = vmData

		actionType := proto.ActionItemDTO_MOVE
		actionItemDTO := &proto.ActionItemDTO{
			ActionType: &actionType,
			TargetSE:   targetSE,
			NewSE:      newSE,
		}

		_, err := actor.ExcuteAction(actionItemDTO, -1)
		if err != nil {
			glog.Errorf("Error executing move: %v", err)
		}
		return
	} else if action == "provision" {
		podName := simulator.Pod()
		podIdentifier := namespace + "/" + podName

		targetSE, _ := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_CONTAINER_POD, podIdentifier).Create()
		newSE := targetSE

		actionType := proto.ActionItemDTO_PROVISION
		actionItemDTO := &proto.ActionItemDTO{
			ActionType: &actionType,
			TargetSE:   targetSE,
			NewSE:      newSE,
		}

		_, err := actor.ExcuteAction(actionItemDTO, -1)
		if err != nil {
			glog.Errorf("Error executing provision: %v", err)
		}
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
		_, err := actor.ExcuteAction(actionItemDTO, -1)
		if err != nil {
			glog.Errorf("Error executing provision: %v", err)
		}
		return
	}
}
