package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	machinev1beta1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	"github.com/openshift/machine-api-operator/pkg/generated/clientset/versioned"
	"github.com/openshift/machine-api-operator/pkg/generated/clientset/versioned/typed/machine/v1beta1"
	"github.com/spf13/viper"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
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
	client      *k8sClusterApi
	machineName string // name of the Machine to be cloned or deleted
	diff        int32  // number of Machines to provision (if diff > 0) or suspend (if diff < 0)
	actionType  ActionType
}

type Controller interface {
	checkPreconditions() error
	checkSuccess() error
	executeAction() error
}

// machineSetController executes a machineSet scaling action request.
type machineSetController struct {
	request     *actionRequest              // The action request
	machineSet  *machinev1beta1.MachineSet  // the MachineSet controlling the machine
	machine     *machinev1beta1.Machine     // The identified Machine, will be used for SUSPEND action
	machineList *machinev1beta1.MachineList // the Machines managed by the MachineSet before action execution, will be used for PROVISION action
}

//
// ------------------------------------------------------------------------------------------------------------------
//

// Check preconditions
func (controller *machineSetController) checkPreconditions() error {
	ok, err := controller.checkMachineSet(controller.machineSet)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("machine set is not in the coherent state")
	}

	resultingReplicas := int(*controller.machineSet.Spec.Replicas) + int(controller.request.diff)

	// Ensure that the resulting replicas do not drop below the minNodes.
	minNodes := getIntOrDefault(cluster.MinNodesConfigKey, cluster.DefaultMinNodePoolSize)
	if resultingReplicas < minNodes {
		return fmt.Errorf("machine set replicas can't be brought down below the minimum nodes of %d", minNodes)
	}

	// Ensure that the resulting replicas do not exceed the maxNodes.
	maxNodes := getIntOrDefault(cluster.MaxNodesConfigKey, cluster.DefaultMaxNodePoolSize)
	if resultingReplicas > maxNodes {
		return fmt.Errorf("machine set replicas can't exceed the maximum nodes of %d", maxNodes)
	}
	return nil
}

// Helper function to get int value from viper or use default
func getIntOrDefault(key string, defaultValue int) int {
	value := viper.GetInt(key)
	if value == 0 {
		return defaultValue
	}
	return value
}

// executeAction scales a MachineSet by modifying its replica count
func (controller *machineSetController) executeAction() error {
	diff := controller.request.diff
	client := controller.request.client
	desiredReplicas := controller.machineSet.Status.Replicas + diff
	controller.machineSet.Spec.Replicas = &desiredReplicas

	if diff < 0 {
		// We need to mark the machine for deletion to ensure this is the
		// one removed by machine controller while scaling down.
		// https://github.com/openshift/machine-api-operator/blob/master/pkg/controller/machineset/delete_policy.go#L34
		machine, err := client.identifyManagingMachine(controller.request.machineName)
		if err != nil {
			return err
		}

		if machine.ObjectMeta.Annotations == nil {
			machine.ObjectMeta.Annotations = make(map[string]string)
		}
		// MachineSet controller does not care what is the value of the string.
		machine.ObjectMeta.Annotations[DeleteNodeAnnotation] = "delete"
		_, err = client.machine.Update(context.TODO(), machine, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	machineSet, err := client.machineSet.Update(context.TODO(), controller.machineSet, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	controller.machineSet = machineSet
	return nil
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
	oldMachine := controller.machine
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
		err = controller.waitForMachineDeprovisioning(oldMachine)
	}
	if err != nil {
		return fmt.Errorf("machine provision/suspend action failed for %s in machineSet %s: %v", oldMachine.Name, machineSet.Name, err)
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

// Construct the controller
func newController(namespace string, nodeName string, diff int32, actionType ActionType,
	clusterScraper *cluster.ClusterScraper,
) (Controller, *string, error) {
	// Check whether Cluster API is enabled.
	if !clusterScraper.IsClusterAPIEnabled() {
		return nil, nil, fmt.Errorf("no Cluster API available")
	}
	// Construct the API clients.
	cApiClient := clusterScraper.CApiClient
	kubeClient := clusterScraper.Clientset
	client := &k8sClusterApi{
		caClient:   cApiClient,
		k8sClient:  kubeClient,
		discovery:  kubeClient.Discovery(),
		machine:    cApiClient.MachineV1beta1().Machines(namespace),
		machineSet: cApiClient.MachineV1beta1().MachineSets(namespace),
	}
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
	machineSet, err := client.machineSet.Get(context.TODO(), ownerInfo.Name, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}

	machineList, err := client.listMachinesInMachineSet(machineSet)
	if err != nil {
		return nil, nil, err
	}

	request := &actionRequest{client, nodeName, diff, actionType}
	return &machineSetController{request, machineSet, machine, machineList}, &ownerInfo.Name, nil
}
