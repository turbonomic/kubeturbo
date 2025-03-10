package executor

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/golang/glog"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	"github.com/openshift/client-go/machine/clientset/versioned"
	"github.com/openshift/client-go/machine/clientset/versioned/typed/machine/v1beta1"
	"github.com/spf13/viper"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"

	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
)

// ActionType describes the current phase of processing the Action request.
type ActionType string

// These are the valid Action types.
const (
	DeleteNodeAnnotation                  = "machine.openshift.io/cluster-api-delete-machine"
	ProvisionAction            ActionType = "Provision"
	SuspendAction              ActionType = "Suspend"
	operationMaxWaits                     = 60
	operationWaitSleepInterval            = 10 * time.Second
	maxReplicaAnnotation                  = "machine.openshift.io/cluster-api-autoscaler-node-group-max-size"
	minReplicaAnnotation                  = "machine.openshift.io/cluster-api-autoscaler-node-group-min-size"
)

// apiClients encapsulates Kubernetes and ClusterAPI clients and interfaces needed for machine scaling.
// ca prefix stands for Cluster API everywhere.
type k8sClusterApi struct {
	// clients
	caClient  *versioned.Clientset
	k8sClient *kubernetes.Clientset

	// Core API Resource client interfaces
	discovery discovery.DiscoveryInterface

	// Cluster API Resource client interfaces
	machine    v1beta1.MachineInterface
	machineSet v1beta1.MachineSetInterface
}

// identifyManagingMachine returns the Machine that manages the given node.
// An error is returned if the Machine is not found or the node does not exist.
func (client *k8sClusterApi) identifyManagingMachine(nodeName string) (*machinev1beta1.Machine, error) {
	// Check if a node with the passed name exists.
	_, err := client.k8sClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		machineName := nodeName
		// We get the machine name as is in the stitched env.
		machine, err := client.machine.Get(context.TODO(), machineName, metav1.GetOptions{})
		if err == nil {
			return machine, nil
		}

		return nil, fmt.Errorf("no node or a machine found named %s: %v ", machineName, err)
	}
	if err != nil {
		return nil, fmt.Errorf("error retrieving node %s: %v", nodeName, err)
	}

	// List all machines and match the node.
	machineList, err := client.machine.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, machine := range machineList.Items {
		if nodeName == machine.Status.NodeRef.Name {
			return &machine, nil
		}
	}
	return nil, fmt.Errorf("Machine not found for the node " + nodeName)
}

// listMachinesInMachineSet lists machines managed by the MachineSet
func (client *k8sClusterApi) listMachinesInMachineSet(ms *machinev1beta1.MachineSet) (*machinev1beta1.MachineList, error) {
	sString := metav1.FormatLabelSelector(&ms.Spec.Selector)
	listOpts := metav1.ListOptions{LabelSelector: sString}
	return client.machine.List(context.TODO(), listOpts)
}

//
// ------------------------------------------------------------------------------------------------------------------
//

// actionRequest represents a single request for action execution.  This is the "base" type for all action requests.
type actionRequest struct {
	client     *k8sClusterApi
	diff       int32 // number of Machines to provision (if diff > 0) or suspend (if diff < 0)
	actionType ActionType
}

type Controller interface {
	checkPreconditions() error
	checkSuccess() error
	executeAction() error
}

type ControllerUitilityInterface interface {
	getMaxReplicas() int
	getMinReplicas() int
	checkMachineSet(args ...interface{}) (bool, error)
}

type machineInfo struct {
	actionItemId int64
	machine      *machinev1beta1.Machine
	actionResult string
}

// machineSetController executes a machineSet scaling action request.
type machineSetController struct {
	request      *actionRequest              // The action request
	machineSet   *machinev1beta1.MachineSet  // the MachineSet controlling the machine
	machinesInfo []*machineInfo              // The identified set of Machines, for the SUSPEND action with action Ids
	machineList  *machinev1beta1.MachineList // the Machines managed by the MachineSet before action execution, will be used for PROVISION action
	// optional Stuff
	controllerName          string
	namespace               string
	controllerUtilityHelper ControllerUitilityInterface // this allows overriding the helper methods in the unit test
}

func (controller *machineSetController) getUtilityHelper() ControllerUitilityInterface {
	if controller.controllerUtilityHelper != nil {
		return controller.controllerUtilityHelper
	}
	return controller
}

// ------------------------------------------------------------------------------------------------------------------
//
// Get max replica values for a machineset. The code prioritizes the values set by the Machine Autoscaler over the
// kubeturbo config.
func (controller *machineSetController) getMaxReplicas() int {
	// check if an openshift MachineAutoscaler exist and has max values.
	// These are set as annotations on the machine set.
	annotations := controller.machineSet.GetAnnotations()
	maxReplicas := annotations[maxReplicaAnnotation]

	var maxNodes int
	if maxReplicas != "" {
		maxNodes, _ = strconv.Atoi(maxReplicas)
		glog.V(4).Infof("openshift MachineAutoscaler set for controller: %s and machineset: %s with max: %d\n", controller.controllerName, controller.machineSet.Name, maxNodes)
	} else {
		// Ensure that the resulting replicas do not exceed the maxNodes.
		maxNodes = configs.GetNodePoolSizeConfigValue(configs.MaxNodesConfigPath, viper.GetString, configs.DefaultMaxNodePoolSize)
		glog.V(4).Infof("%s: %s: %d\n", controller.controllerName, configs.MaxNodesConfigPath, maxNodes)
	}

	return maxNodes
}

// ------------------------------------------------------------------------------------------------------------------
//
// Get min replica values for a machineset. The code prioritizes the values set by the Machine Autoscaler over the
// kubeturbo config.
func (controller *machineSetController) getMinReplicas() int {
	// check if an openshift MachineAutoscaler exist and has min values.
	// These are set as annotations on the machine set.
	annotations := controller.machineSet.GetAnnotations()
	minReplicas := annotations[minReplicaAnnotation]

	var minNodes int
	if minReplicas != "" {
		minNodes, _ = strconv.Atoi(minReplicas)
		glog.V(4).Infof("openshift MachineAutoscaler set for controller: %s and machineset: %s with min: %d\n", controller.controllerName, controller.machineSet.Name, minNodes)
	} else {
		// Ensure that the resulting replicas do not drop below the minNodes.
		minNodes := configs.GetNodePoolSizeConfigValue(configs.MinNodesConfigPath, viper.GetString, configs.DefaultMinNodePoolSize)
		glog.V(4).Infof("%s: %s: %d\n", controller.controllerName, configs.MinNodesConfigPath, minNodes)
	}

	return minNodes
}

// ------------------------------------------------------------------------------------------------------------------
//
// Check preconditions
func (controller *machineSetController) checkPreconditions() error {
	ok, err := controller.getUtilityHelper().checkMachineSet(controller.machineSet)

	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("machine set is not in the coherent state")
	}

	currentReplicas := int(*controller.machineSet.Spec.Replicas)
	diff := int(controller.request.diff)
	resultingReplicas := currentReplicas + diff
	glog.V(4).Infof("Current Replicas: %d, Request Diff: %d and Resulting Replicas: %d", currentReplicas, diff, resultingReplicas)

	maxNodes := controller.getUtilityHelper().getMaxReplicas()
	minNodes := controller.getUtilityHelper().getMinReplicas()

	// Ensure that the resulting replicas do not drop below the minNodes or exceed the maxNodes.
	if diff < 0 && resultingReplicas < minNodes {
		err = fmt.Errorf("machine set replicas can't be brought down below the minimum nodes of %d", minNodes)
		acceptableDiff := currentReplicas - minNodes
		for i, machineInfo := range controller.machinesInfo {
			if i >= acceptableDiff { // fail the remaining machines
				machineInfo.actionResult = err.Error()
			}
		}
		if acceptableDiff > 0 {
			controller.request.diff = int32(-acceptableDiff)
		} else {
			return err
		}
	}
	if diff > 0 && resultingReplicas > maxNodes {
		err = fmt.Errorf("machine set replicas can't exceed the maximum nodes of %d", maxNodes)
		if currentReplicas < maxNodes { // don't give up this easily
			controller.request.diff = int32(maxNodes - currentReplicas)
		} else {
			return err
		}
	}
	return nil
}

// executeAction scales a MachineSet by modifying its replica count
func (controller *machineSetController) executeAction() error {
	err := controller.checkPreconditions()
	if err != nil {
		return err
	}

	client := controller.request.client
	diff := controller.request.diff
	desiredReplicas := controller.machineSet.Status.Replicas + diff

	defer func() {
		if diff < 0 {
			for _, machineInfo := range controller.machinesInfo {
				if machineInfo.actionResult != actionResultSuccess {
					markMachineForDeletion(machineInfo.machine, client, false)
				}
			}
		}
	}()

	if diff < 0 {
		for _, machineInfo := range controller.machinesInfo {
			if machineInfo.actionResult == actionResultSuccess {
				err = markMachineForDeletion(machineInfo.machine, client, true)
				if err != nil {
					machineInfo.actionResult = err.Error()
				}
			}
		}
	}

	controller.machineSet.Spec.Replicas = &desiredReplicas
	updatedMachineSet, err := client.machineSet.Update(context.TODO(), controller.machineSet, metav1.UpdateOptions{})
	if err != nil {
		glog.V(3).Infof("%s: error while updating machine set: %v", controller.controllerName, err)
		for _, machineInfo := range controller.machinesInfo {
			machineInfo.actionResult = err.Error()
		}
		return err
	}
	controller.machineSet = updatedMachineSet

	glog.V(3).Infof("[%s] changed replicas to %d, Status %d",
		controller.controllerName, *controller.machineSet.Spec.Replicas, controller.machineSet.Status.Replicas)

	glog.V(4).Infof("Start check node suspension success nodes for controller %s", controller.controllerName)
	err = controller.checkSuccess()

	return err
}

// stateCheck checks for a state.
type stateCheck func(...interface{}) (bool, error)

// checkMachineSet checks whether current replica set matches the list of alive machines.
func (controller *machineSetController) checkMachineSet(args ...interface{}) (bool, error) {
	machineSet := args[0].(*machinev1beta1.MachineSet)
	if machineSet.Spec.Replicas == nil {
		return false, fmt.Errorf("MachineSet %s invalid replica count (nil)", machineSet.Name)
	}
	// get MachineSet's list of managed Machines
	machineList, err := controller.request.client.listMachinesInMachineSet(machineSet)
	if err != nil {
		return false, err
	}
	// Filter dead machines.
	alive := 0
	for _, machine := range machineList.Items {
		if machine.DeletionTimestamp == nil {
			alive++
		}
	}
	// Check replica count match with the number of managed machines.
	if int(*machineSet.Spec.Replicas) != alive {
		return false, nil
	}
	return true, nil
}

// identifyDiff locates machine in list1 which is not in list2.
// list1 should always have 1 machine more then list2.
func (controller *machineSetController) identifyDiff(list1, list2 *machinev1beta1.MachineList) *machinev1beta1.Machine {
	for _, machine1 := range list1.Items {
		found := false
		for _, machine2 := range list2.Items {
			if machine1.Name == machine2.Name {
				found = true
				break
			}
		}
		if found {
			continue
		} else {
			return &machine1
		}
	}
	return nil
}

// checkSuccess verifies that the action has been successful.
func (controller *machineSetController) checkSuccess() error {
	machineSet := controller.machineSet
	stateDesc := fmt.Sprintf("MachineSet %s contains %d Machines", machineSet.Name, *machineSet.Spec.Replicas)
	// This step waits until after replica update, the list of machines matches the replicas.
	err := controller.waitForState(stateDesc, controller.checkMachineSet, machineSet)
	if err != nil {
		return err
	}
	// get post-Action list of Machines in the MachineSet
	machineList, err := controller.request.client.listMachinesInMachineSet(machineSet)
	if err != nil {
		return err
	}

	if controller.request.actionType == ProvisionAction {
		// Identify the extra machine.
		newMachine := controller.identifyDiff(machineList, controller.machineList)
		if newMachine == nil {
			return fmt.Errorf("no new machine has been identified for machineSet %v", machineSet)
		}
		err = controller.waitForMachineProvisioning(newMachine)
	} else {
		var wg sync.WaitGroup
		for _, machineInfo := range controller.machinesInfo {
			oldMachineInfo := machineInfo
			machineName := oldMachineInfo.machine.Name
			if oldMachineInfo.actionResult != actionResultSuccess {
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				glog.V(4).Infof("Wait for machine deprovisioning for %s", machineName)
				err = controller.waitForMachineDeprovisioning(oldMachineInfo.machine)
				if err != nil {
					glog.V(3).Infof("machine suspend action failed for %s in machineSet %s: %v", machineName, machineSet.Name, err)
					oldMachineInfo.actionResult = err.Error()
				}
			}()
		}
		wg.Wait()
	}
	if err != nil {
		return fmt.Errorf("machine provision/suspend action failed in machineSet %s: %v", machineSet.Name, err)
	}
	return nil
}

// checkMachineSuccess checks whether machine has been created successfully.
func (controller *machineSetController) checkMachineSuccess(args ...interface{}) (bool, error) {
	machineName := args[0].(string)
	machine, err := controller.request.client.machine.Get(context.TODO(), machineName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if machine.ObjectMeta.CreationTimestamp.String() != "" && machine.Status.ErrorMessage == nil {
		return true, nil
	}
	return false, nil
}

// isMachineReady checks whether the machine is ready.
func (controller *machineSetController) isMachineReady(args ...interface{}) (bool, error) {
	machineName := args[0].(string)
	machine, err := controller.request.client.machine.Get(context.TODO(), machineName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if machine.Status.ErrorMessage != nil {
		return true, nil
	}
	return true, nil
}

// waitForMachineProvisioning waits for the new machine to be provisioned with timeout.
func (controller *machineSetController) waitForMachineProvisioning(newMachine *machinev1beta1.Machine) error {
	descr := fmt.Sprintf("machine %s Machine creation status is final", newMachine.Name)
	err := controller.waitForState(descr, controller.checkMachineSuccess, newMachine.Name)
	if err != nil {
		return err
	}
	machine, err := controller.request.client.machine.Get(context.TODO(), newMachine.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if machine.Status.ErrorMessage != nil {
		err = fmt.Errorf("machine %s failed to create new Machine: %v: %s",
			newMachine.Name, newMachine.Status.ErrorReason, *newMachine.Status.ErrorMessage)
		return err
	}
	newNName := newMachine.ObjectMeta.Name
	// wait for new Machine to be in Ready state
	descr = fmt.Sprintf("machine %s is Ready", newNName)
	return controller.waitForState(descr, controller.isMachineReady, newNName)
}

// isMachineDeletedOrNotReady checks whether the machine is deleted or not ready.
func (controller *machineSetController) isMachineDeleted(args ...interface{}) (bool, error) {
	machineName := args[0].(string)
	_, err := controller.request.client.machine.Get(context.TODO(), machineName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return true, nil
	}
	if err != nil {
		// Error in retrieving the machine, generally such an error except notfound is not
		// transient and should be reported.
		return false, err
	}

	// TODO: We can put an additional check in future to validate the node also vanishes
	return false, nil
}

// waitForMachineDeprovisioning waits for the new machine to be de-provisioned with timeout.
func (controller *machineSetController) waitForMachineDeprovisioning(machine *machinev1beta1.Machine) error {
	deletedName := machine.Name
	descr := fmt.Sprintf("machine %s deleted or exited Ready state", deletedName)
	return controller.waitForState(descr, controller.isMachineDeleted, deletedName)
}

// waitForState Is the function that allows to wait for a specific state, or until it times out.
func (controller *machineSetController) waitForState(stateDesc string, f stateCheck, args ...interface{}) error {
	glog.V(4).Infof("Waiting for state: '%s'", stateDesc)
	for i := 0; i < operationMaxWaits; i++ {
		ok, err := f(args...)
		if err != nil {
			return fmt.Errorf("error while waiting for state %v", err)
		}
		// We are done, return
		if ok {
			glog.V(4).Infof("Verified desired state: '%s'", stateDesc)
			return nil
		}
		time.Sleep(operationWaitSleepInterval)
	}
	return fmt.Errorf("cannot verify %s: timed out after %v",
		stateDesc, time.Duration(operationMaxWaits)*operationWaitSleepInterval)
}

func constructAPIclient(clusterScraper *cluster.ClusterScraper, namespace string) (*k8sClusterApi, error) {
	// Check whether Cluster API is enabled.
	isEnabled, err := clusterScraper.IsClusterAPIEnabled()
	if !isEnabled {
		return nil, fmt.Errorf("no Cluster API available. %s", err)
	}
	// Construct the API client.
	cApiClient := clusterScraper.CApiClient
	kubeClient := clusterScraper.Clientset

	return &k8sClusterApi{
		caClient:   cApiClient,
		k8sClient:  kubeClient,
		discovery:  kubeClient.Discovery(),
		machine:    cApiClient.MachineV1beta1().Machines(namespace),
		machineSet: cApiClient.MachineV1beta1().MachineSets(namespace),
	}, nil
}

func getMachineByNodeName(nodeName string, client *k8sClusterApi) (*machinev1beta1.Machine, *string, error) {
	// Identify managing machine.
	machine, err := client.identifyManagingMachine(nodeName)
	if err != nil {
		err = fmt.Errorf("cannot identify managing machine: %v", err)
		return nil, nil, err
	}
	glog.V(3).Infof("Identified %s as managing machine for %v", machine.Name, nodeName)
	ownerInfo, ownerSet := util.GetOwnerInfo(machine.OwnerReferences)
	if !ownerSet {
		return nil, nil, fmt.Errorf("ownerRef missing from machine %s which manages %s", machine.Name, nodeName)
	}
	// TODO: Watch cluster-api evolution and check implementers other then openshift
	// for a more generic implementation.
	// In openshift we assume that machines are managed by machinesets.
	if ownerInfo.Kind != "MachineSet" {
		return nil, nil, fmt.Errorf("invalid owner kind [%s] for machine %s which manages %s",
			ownerInfo.Kind, machine.Name, nodeName)
	}
	return machine, &ownerInfo.Name, nil
}

func markMachineForDeletion(machine *machinev1beta1.Machine, client *k8sClusterApi, annotate bool) error {
	// We need to mark the machine for deletion to ensure this is the
	// one removed by machine controller while scaling down.
	// https://github.com/openshift/machine-api-operator/blob/master/pkg/controller/machineset/delete_policy.go#L34
	if machine.ObjectMeta.Annotations == nil {
		if !annotate {
			return nil
		}
		machine.ObjectMeta.Annotations = make(map[string]string)
	}
	// MachineSet controller does not care what is the value of the string.
	if !annotate {
		delete(machine.ObjectMeta.Annotations, DeleteNodeAnnotation)
	} else {
		machine.ObjectMeta.Annotations[DeleteNodeAnnotation] = "delete"
	}

	_, err := client.machine.Update(context.TODO(), machine, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("error while updating machine %s: %v", machine.Name, err)
	}
	glog.V(4).Infof("Updated machine %s with delete annotation %s", machine.Name, DeleteNodeAnnotation)

	return nil
}

// Construct the controller
func (s *MachineActionExecutor) createSingleNodeController(nodeName string, diff int32, actionItemId int64) (*machineSetController, error) {
	client, err := constructAPIclient(s.executor.clusterScraper, s.cAPINamespace)
	if err != nil {
		return nil, err
	}

	machine, ownerName, err := getMachineByNodeName(nodeName, client)
	if err != nil {
		return nil, err
	}

	machineSet, err := client.machineSet.Get(context.TODO(), *ownerName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	machineList, err := client.listMachinesInMachineSet(machineSet)
	if err != nil {
		return nil, err
	}

	var actionType ActionType
	if diff > 0 {
		actionType = ProvisionAction
	} else {
		actionType = SuspendAction
	}
	request := &actionRequest{client, diff, actionType}
	return &machineSetController{
			request, machineSet,
			[]*machineInfo{{actionItemId, machine, actionResultSuccess}},
			machineList, *ownerName, s.cAPINamespace, nil,
		},
		nil
}

func (s *MachineActionExecutor) createMultiNodeController(actionItemIdNodeName *map[int64]string) ([]*machineSetController, error) {
	client, err := constructAPIclient(s.executor.clusterScraper, s.cAPINamespace)
	if err != nil {
		return nil, err
	}

	machinesByMachineSet := make(map[string][]*machineInfo)
	machineSetMap := make(map[string]*machinev1beta1.MachineSet)

	for actionItemId, nodeName := range *actionItemIdNodeName {
		// Identify managing machine and the machine set
		machine, ownerName, err := getMachineByNodeName(nodeName, client)
		if err != nil {
			return nil, err
		}

		machineSet, err := client.machineSet.Get(context.TODO(), *ownerName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		machineSetMap[machineSet.Name] = machineSet

		if _, ok := machinesByMachineSet[machineSet.Name]; !ok {
			machinesByMachineSet[machineSet.Name] = []*machineInfo{}
		}
		machinesByMachineSet[machineSet.Name] = append(machinesByMachineSet[machineSet.Name],
			&machineInfo{actionItemId, machine, actionResultSuccess})
	}

	var controllerList []*machineSetController
	for machineSetName, machinesInfo := range machinesByMachineSet {

		machineSet := machineSetMap[machineSetName]
		machineList, err := client.listMachinesInMachineSet(machineSet)
		if err != nil {
			return nil, err
		}
		// negative sign because it is only deprovisioning
		diff := -int32(len(machinesInfo))
		request := &actionRequest{client, diff, SuspendAction}
		controllerName := fmt.Sprintf("[%s]", machineSet.Name)
		glog.V(4).Infof("created new multi node machine set action controller for %s to scale down by %d nodes", controllerName, diff)
		controller := &machineSetController{
			request, machineSet,
			machinesInfo,
			machineList, controllerName, s.cAPINamespace, nil,
		}

		controllerList = append(controllerList, controller)
	}

	return controllerList, nil
}
