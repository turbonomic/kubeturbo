/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cluster

import (
	"flag"
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
	turboexec "github.com/turbonomic/kubeturbo/pkg/action/executor"
	"k8s.io/client-go/kubernetes"
	clusterclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	clusterapiutil "sigs.k8s.io/cluster-api/pkg/util"
)

type ScaleOptions struct {
	scaleOut   bool
	kubeConfig string
	nodeName   string
	verbo      string
}

var ro = &ScaleOptions{}

var rootCmd = &cobra.Command{
	Use:   "nodescaler",
	Short: "Scale node",
	Long:  `Scales given node`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := RunScale(ro); err != nil {
			glog.Exit(err)
		}
	},
}

func RunScale(ro *ScaleOptions) error {
	// set the "log verbosity" flag
	vString := "-v=" + ro.verbo
	err := flag.CommandLine.Parse([]string{vString})
	if err != nil {
		return err
	}

	// resolve kube-config path
	if ro.kubeConfig == "" {
		ro.kubeConfig = clusterapiutil.GetDefaultKubeConfigPath()
	}

	// create client for core kubernetes API
	// TODO: add code to construct a rest.Config as the argument below
	kubeClient, err := kubernetes.NewForConfig(nil)
	if err != nil {
		err = fmt.Errorf("error creating kube client set: %v", err)
		return err
	}

	// create client for Cluster API
	c, err := clusterclient.NewForConfig(nil)
	if err != nil {
		err = fmt.Errorf("error creating Cluster API client set: %v", err)
		return err
	}

	//
	// Test entities
	//

	// TODO: Construct the following properly
	//actionItemDTO := ro.nodeName
	actionItemDTO := &turboexec.TurboActionExecutorInput{}

	tracker := "replace this string with tracker object when integrated"
	var executor turboexec.ScaleActionExecutor
	if ro.scaleOut {
		executor, err = turboexec.NewNodeProvisioner(c, kubeClient)
	} else {
		executor, err = turboexec.NewNodeSuspender(c, kubeClient)
	}
	if err != nil {
		return err
	}

	// TODO: When integrating with kubeturbo:
	// TODO:   Consider using a switch based on ActionExecutor type to completely ignore the kubeturbo-based
	// TODO:   TurboExecutor code from the very outset, even for the action keep alive and action locking.

	//
	// Action keepAlive: Create progress indicator to send frequent updates to Turbo server to prevent Turbo timeout
	//

	// create progress indicator
	progress := turboexec.NewProgress(tracker)

	// start keepAlive
	stop := make(chan struct{})
	defer close(stop)
	go progress.KeepAlive(turboexec.ActionKeepAliveInterval, stop)

	//
	// Action lock: Lock the action to the ScalingController
	//

	// identify the Node and its ScalingController and get the Controller's key with which to lock the action
	err = progress.Update(0, "Identifying ScalingController managing the Node", turboexec.ActionLockAcquisition)
	if err != nil {
		return err
	}

	lockKey, err := turboexec.GetLockKey(executor, actionItemDTO, progress)
	if err != nil {
		return err
	}

	// TODO: When integrating with kubeturbo:
	// TODO:   1. Add nodeLockStore to ActionHandler struct
	// TODO:   2. Add new signature to IActionLockStore interface:
	// TODO:         getLockWithKey(key string) (*util.LockHelper, error)
	// TODO:   2. Implement ActionLockStore.getLockWithKey(key string) modeled after
	// TODO:      ActionLockStore.getLock(actionItem *proto.ActionItemDTO)
	// TODO:   3. Modify ActionHandler.execute(): When executor type is a ScalingExecutor:
	// TODO:         lockKey := util.GetLockKey(executor, actionItemDTO, progress)
	// TODO:         getLockWithKey(lockKey)

	// lock scaling action to the ScalingController to prevent concurrent scaling actions
	err = progress.Update(0, fmt.Sprintf("Acquiring Action lock: key=\"%s\" (ScalingController)", lockKey), turboexec.ActionLockAcquisition)
	if err != nil {
		return err
	}

	err = turboexec.AcquireLock(lockKey)
	if err != nil {
		return err
	}
	defer turboexec.ReleaseLock(lockKey) // release lock when Action Execution terminates for any reason

	//
	// Execute Scaling Action: Call ScaleActionExecutor.Execute()
	//

	// execute scaling action
	// TODO: Either:
	// TODO:   1. Modify kubeturbo TurboActionExecutor.Execute() method, adding progress indicator argument
	// TODO:   2. Create kubeturbo TurboNodeActionExecutor.Execute() method, adding progress indicator argument
	//_, err = r.Execute(actionItemDTO, progress)
	//_, err = r.ExecuteWithProgress(actionItemDTO, progress)
	switch executor := executor.(type) {
	case *turboexec.Provisioner:
		_, err = executor.ExecuteWithProgress(actionItemDTO, progress, lockKey)
	case *turboexec.Suspender:
		_, err = executor.ExecuteWithProgress(actionItemDTO, progress, lockKey)
	default:
		glog.Errorf("Unsupported executor type: %s", executor)
	}
	time.Sleep(1 * time.Second)
	return err
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		glog.Exit(err)
	}
}

func init() {
	rootCmd.Flags().BoolVarP(&ro.scaleOut, "scaleout", "s", true, "scaling direction: out (true) or in (false)")
	rootCmd.PersistentFlags().StringVarP(&ro.kubeConfig, "kubeconfig", "k", "", "location of kubernetes config file (default $HOME/.kube/config)")
	rootCmd.PersistentFlags().StringVarP(&ro.nodeName, "nodename", "n", "", "node to be cloned")
	rootCmd.PersistentFlags().StringVarP(&ro.verbo, "verbo", "b", "1", "verbo level")
	//flag.CommandLine.Parse([]string{})
	//flag.CommandLine.Parse([]string{"-v=2"})
	// TODO: I'm commenting out the line below to avoid the "flag redefined: log_dir" error during go test
	//logs.InitLogs()
}
