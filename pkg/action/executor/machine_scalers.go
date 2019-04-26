package executor

import (
	"fmt"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/typed/cluster/v1alpha1"
	"time"
)

// ActionType describes the current phase of processing the Action request.
type ActionType string

// These are the valid Action types.
const (
	clusterAPIGroupVersion                = "cluster.k8s.io/v1alpha1"
	clusterAPINamespace                   = v1.NamespaceDefault
	nodeAnnotationMachine                 = "machine"
	ProvisionAction            ActionType = "Provision"
	SuspendAction              ActionType = "Suspend"
	operationMaxWaits                     = 60
	operationWaitSleepInterval            = 10 * time.Second
)

// apiClients encapsulates Kubernetes and ClusterAPI clients and interfaces needed for Node scaling.
// ca prefix stands for Cluster API everywhere.
type k8sClusterApi struct {
	// clients
	caClient  *clientset.Clientset
	k8sClient *kubernetes.Clientset

	// Core API Resource client interfaces
	discovery discovery.DiscoveryInterface
	node      clientcorev1.NodeInterface

	// Cluster API Resource client interfaces
	machine           v1alpha1.MachineInterface
	machineSet        v1alpha1.MachineSetInterface
	machineDeployment v1alpha1.MachineDeploymentInterface

	caGroupVersion string // clusterAPI group and version
}

// verifyClusterAPIEnabled Checks whether Cluster API is enabled.
func (client *k8sClusterApi) verifyClusterAPIEnabled() error {
	serviceString := fmt.Sprintf("ClusterAPI service \"%s\"", client.caGroupVersion)
	_, err := client.discovery.ServerResourcesForGroupVersion(client.caGroupVersion)
	if err != nil {
		err := fmt.Errorf("%s is not available: %v", serviceString, err)
		return err
	}
	return nil
}

// identifyManagingMachine returns the Machine that manages a Node.  The Machine name is located in a Node annotation.
// An error is returned if the Node, Machine or Node annotation is not found.
func (client *k8sClusterApi) identifyManagingMachine(nodeName string) (*clusterv1.Machine, error) {
	node, err := client.node.Get(nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	// get the Machine name from a Node annotation
	machineName, ok := node.Annotations[nodeAnnotationMachine]
	if !ok {
		return nil, fmt.Errorf("\"%s\" annotation not found on Node %s", nodeAnnotationMachine, nodeName)
	}
	// Verify that a Machine exists.
	machine, err := client.machine.Get(machineName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return machine, nil
}

// identifyManagingMachineSet returns the MachineSet that manages a specific Machine and the complete list of Machines
// managed by that MachineSet. Returns an error if the Machine is not managed by a MachineSet.
func (client *k8sClusterApi) identifyManagingMachineSet(machineName string) (*clusterv1.MachineSet, *clusterv1.MachineList, error) {
	// retrieve all MachineSets in the cluster
	msList, err := client.machineSet.List(metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	// iterate through MachineSets to find the one containing our Machine
	var machineSet clusterv1.MachineSet
	var machineList *clusterv1.MachineList
	for _, machineSet = range msList.Items {
		// retrieve all Machines in this MachineSet
		machineList, err = client.listMachinesInSet(&machineSet)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot retrieve Machines in MachineSet %s: %v", machineSet.Name, err)
		}
		// check for our Machine in the list
		for _, m := range machineList.Items {
			if machineName == m.Name {
				return &machineSet, machineList, nil
			}
		}
	}
	err = fmt.Errorf("machine %s is not managed by a MachineSet", machineSet.Name)
	return nil, nil, err
}

// listMachinesInSet makes an API call and returns the list of Machines in a MachineSet, i.e., the list of the
// Machines whose labels match on MachineSet.Spec.Selector.
func (client *k8sClusterApi) listMachinesInSet(ms *clusterv1.MachineSet) (*clusterv1.MachineList, error) {
	sString := metav1.FormatLabelSelector(&ms.Spec.Selector)
	listOpts := metav1.ListOptions{LabelSelector: sString}
	return client.machine.List(listOpts)
}

//
// ------------------------------------------------------------------------------------------------------------------
//

// actionRequest represents a single request for action execution.  This is the "base" type for all action requests.
type actionRequest struct {
	client     *k8sClusterApi
	nodeName   string // name of the Node to be cloned or deleted
	diff       int32  // number of Nodes to provision (if diff > 0) or suspend (if diff < 0)
	actionType ActionType
}

type controller interface {
	checkPreconditions() error
	checkSuccess() error
	executeAction() error
	getLock() string
}

// machineSetController executes a MachineSet scaling action request.
type machineSetController struct {
	request     *actionRequest         // The action request
	machineSet  *clusterv1.MachineSet  // the MachineSet controlling the Node
	machineList *clusterv1.MachineList // the Machines managed by the MachineSet before action execution
}

//
// ------------------------------------------------------------------------------------------------------------------
//

// getLock returns the lock to be checked for machine sets.
func (controller *machineSetController) getLock() string {
	return controller.machineSet.Name
}

// Check preconditions
func (controller *machineSetController) checkPreconditions() error {
	if controller.machineSet.Spec.Replicas == nil {
		return fmt.Errorf("MachineSet %s invalid replica count (nil)", controller.machineSet.Name)
	}
	// get MachineSet's list of managed Machines
	machineList, err := controller.request.client.listMachinesInSet(controller.machineSet)
	if err != nil {
		return err
	}
	// Check replica count match with the number of managed machines.
	if int(*controller.machineSet.Spec.Replicas) != len(machineList.Items) {
		return fmt.Errorf("MachineSet %s replica count doesn't match the machine count: %d vs. %d",
			controller.machineSet.Name, int(*controller.machineSet.Spec.Replicas), len(machineList.Items))
	}
	if int(*controller.machineSet.Spec.Replicas+controller.request.diff) < 1 {
		return fmt.Errorf("MachineSet %s must be left with at least a single node", controller.machineSet.Name)
	}
	return nil
}

// executeAction scales a MachineSet by modifying its replica count
func (controller *machineSetController) executeAction() error {
	desiredReplicas := controller.machineSet.Status.Replicas + controller.request.diff
	controller.machineSet.Status.Replicas = desiredReplicas
	machineSet, err := controller.request.client.machineSet.Update(controller.machineSet)
	if err != nil {
		return err
	}
	// Save a new machine set
	controller.machineSet = machineSet
	return nil
}

// stateCheck checks for a state.
type stateCheck func(...interface{}) error

func (controller *machineSetController) checkMachineSet(args ...interface{}) error {
	machineSet := args[0].(*clusterv1.MachineSet)
	if machineSet.Spec.Replicas == nil {
		return fmt.Errorf("MachineSet %s invalid replica count (nil)", machineSet.Name)
	}
	// get MachineSet's list of managed Machines
	machineList, err := controller.request.client.listMachinesInSet(machineSet)
	if err != nil {
		return err
	}
	// Check replica count match with the number of managed machines.
	if int(*machineSet.Spec.Replicas) != len(machineList.Items) {
		return fmt.Errorf("")
	}
	return nil
}

// checkSuccess verifies that the action has been successful.
func (controller *machineSetController) checkSuccess() error {
	stateDesc := fmt.Sprintf("MachineSet %s contains %d Machines", controller.machineSet.Name, *controller.machineSet.Spec.Replicas)
	err := controller.waitForState(stateDesc, controller.checkMachineSet, controller.machineSet)
	if err != nil {
		return err
	}
	// get post-Action list of Machines in the MachineSet
	machineList, err := controller.request.client.listMachinesInSet(controller.machineSet)
	if err != nil {
		return err
	}
	// Identify the extra machine.
	var newMachine *clusterv1.Machine
	for _, machineToLocate := range machineList.Items {
		s1Found := false
		for _, machine := range controller.machineList.Items {
			if machineToLocate.Name == machine.Name {
				s1Found = true
				break
			}
		}
		if !s1Found {
			newMachine = &machineToLocate
		}
	}
	if newMachine == nil {
		return fmt.Errorf("no new machine has been identified for MachineSet %v", controller.machineSet)
	}
	// Wait for the node provisioning.
	if controller.request.actionType == ProvisionAction {
		err = controller.waitForNodeProvisioning(newMachine)
	} else {
		err = controller.waitForNodeDeprovisioning(newMachine)
	}
	if err != nil {
		return fmt.Errorf("machine %s failed to provision new Node in MachineSet %s: %v", newMachine.Name, controller.machineSet.Name, err)
	}
	return nil
}

// checkMachineSuccess checks whether machine has been created successfully.
func (controller *machineSetController) checkMachineSuccess(args ...interface{}) error {
	machineName := args[0].(string)
	machine, err := controller.request.client.machine.Get(machineName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if machine.Status.NodeRef != nil && machine.Status.ErrorMessage == nil {
		return nil
	}
	return fmt.Errorf("error obtaining status for a machine %s", machineName)
}

// isNodeReady checks whether the node is ready.
func (controller *machineSetController) isNodeReady(args ...interface{}) error {
	nodeName := args[0].(string)
	node, err := controller.request.client.node.Get(nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	for _, condition := range node.Status.Conditions {
		if condition.Type == v1.NodeReady {
			if condition.Status == v1.ConditionTrue {
				return nil
			}
		}
	}
	return fmt.Errorf("the node %s isn't ready yet", nodeName)
}

// waitForNodeProvisioning waits for the new node to be provisioned with timeout.
func (controller *machineSetController) waitForNodeProvisioning(newMachine *clusterv1.Machine) error {
	descr := fmt.Sprintf("machine %s Node creation status is final", newMachine.Name)
	err := controller.waitForState(descr, controller.checkMachineSuccess, newMachine.Name)
	if err != nil {
		return err
	}
	machine, err := controller.request.client.machine.Get(newMachine.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if machine.Status.ErrorMessage != nil {
		err = fmt.Errorf("machine %s failed to create new Node: %v: %s",
			newMachine.Name, newMachine.Status.ErrorReason, *newMachine.Status.ErrorMessage)
		return err
	}
	newNName := newMachine.Status.NodeRef.Name
	// wait for new Node to be in Ready state
	descr = fmt.Sprintf("node %s is Ready", newNName)
	return controller.waitForState(descr, controller.isNodeReady, newNName)
}

// isNodeDeletedOrNotReady checks whether the node is deleted or not ready.
func (controller *machineSetController) isNodeDeletedOrNotReady(args ...interface{}) error {
	err := controller.isNodeReady(args)
	if err != nil {
		return nil
	}
	return fmt.Errorf("the node isn't ready yet")
}

// waitForNodeDeprovisioning waits for the new node to be de-provisioned with timeout.
func (controller *machineSetController) waitForNodeDeprovisioning(machine *clusterv1.Machine) error {
	deletedNName := machine.Status.NodeRef.Name
	descr := fmt.Sprintf("node %s deleted or exited Ready state", deletedNName)
	return controller.waitForState(descr, controller.isNodeDeletedOrNotReady, deletedNName)
}

// waitForState Is the function that allows to wait for a specific state, or until it times out.
func (controller *machineSetController) waitForState(stateDesc string, f stateCheck, args ...interface{}) error {
	for i := 0; i < operationMaxWaits; i++ {
		// err := progress.Update(progStart+int(progInc*float32(numWaits)), "Verifying "+stateDesc)
		if err := f(args...); err != nil {
			return err
		}
		time.Sleep(operationWaitSleepInterval)
	}
	return fmt.Errorf("cannot verify %s: timed out after %v",
		stateDesc, time.Duration(operationMaxWaits)*operationWaitSleepInterval)
}

// Construct the controller
func newController(nodeName string, diff int32, actionType ActionType,
	cApiClient *clientset.Clientset, kubeClient *kubernetes.Clientset) (controller, error) {
	// Construct the API clients.
	client := &k8sClusterApi{
		caClient:          cApiClient,
		k8sClient:         kubeClient,
		discovery:         kubeClient.Discovery(),
		node:              kubeClient.CoreV1().Nodes(),
		machine:           cApiClient.ClusterV1alpha1().Machines(clusterAPINamespace),
		machineSet:        cApiClient.ClusterV1alpha1().MachineSets(clusterAPINamespace),
		machineDeployment: cApiClient.ClusterV1alpha1().MachineDeployments(clusterAPINamespace),
		caGroupVersion:    clusterAPIGroupVersion,
	}
	// Check whether Cluster API is enabled.
	if err := client.verifyClusterAPIEnabled(); err != nil {
		return nil, fmt.Errorf("cluster API is not enabled for %s: %v", nodeName, err)
	}
	// Identify managing machine.
	machine, err := client.identifyManagingMachine(nodeName)
	if err != nil {
		err = fmt.Errorf("cannot identify machine: %v", err)
		return nil, err
	}
	machineSet, mList, err := client.identifyManagingMachineSet(machine.Name)
	if err != nil {
		err = fmt.Errorf("cannot identify machine set: %v", err)
		return nil, err
	}
	request := &actionRequest{client, nodeName, diff, actionType}
	return &machineSetController{request, machineSet, mList}, nil
}
