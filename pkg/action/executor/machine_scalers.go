package executor

import (
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/typed/cluster/v1alpha1"
	"strings"
	"time"
)

// ActionType describes the current phase of processing the Action request.
type ActionType string

// These are the valid Action types.
const (
	clusterAPIGroupVersion                = "cluster.k8s.io/v1alpha1"
	clusterAPINamespace                   = "kube-system"
	ProvisionAction            ActionType = "Provision"
	SuspendAction              ActionType = "Suspend"
	operationMaxWaits                     = 60
	operationWaitSleepInterval            = 10 * time.Second
)

// apiClients encapsulates Kubernetes and ClusterAPI clients and interfaces needed for NodeF scaling.
// ca prefix stands for Cluster API everywhere.
type k8sClusterApi struct {
	// clients
	caClient  *clientset.Clientset
	k8sClient *kubernetes.Clientset

	// Core API Resource client interfaces
	discovery discovery.DiscoveryInterface

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
	nodes := client.k8sClient.CoreV1().Nodes()
	node, err := nodes.Get(nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	nodeSpecTmp := strings.Split(node.Spec.ProviderID, "/")
	if len(nodeSpecTmp) < 2 {
		return nil, fmt.Errorf("Node " + nodeName + " has no valid provider ID")
	}
	nodeProviderID := nodeSpecTmp[len(nodeSpecTmp)-1]
	// List all machines and match.
	machineList, err := client.machine.List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, machine := range machineList.Items {
		machineSpecTmp := strings.Split(*machine.Spec.ProviderID, "/")
		if len(nodeSpecTmp) < 2 {
			return nil, fmt.Errorf("Machine " + machine.Name + " has no valid provider ID")
		}
		machineProviderID := machineSpecTmp[len(machineSpecTmp)-1]
		if machineProviderID == nodeProviderID {
			return &machine, nil
		}
	}
	return nil, fmt.Errorf("Machine not found for the node " + nodeName)
}

// identifyManagingMachineSet returns the MachineSet that manages a specific Machine and the complete list of Machines
// managed by that MachineSet. Returns an error if the Machine is not managed by a MachineSet.
func (client *k8sClusterApi) identifyManagingMachineSet(machineName string) (*clusterv1.MachineDeployment, *clusterv1.MachineList, error) {
	mdList, err := client.machineDeployment.List(metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}
	var machineList *clusterv1.MachineList
	var machineDeployment clusterv1.MachineDeployment
	for _, machineDeployment = range mdList.Items {
		// retrieve all Machines in this MachineSet
		machineList, err = client.listMachinesInDeployment(&machineDeployment)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot retrieve Machines in MachineDeployment %s: %v", machineDeployment.Name, err)
		}
		// check for our Machine in the list
		for _, m := range machineList.Items {
			if machineName == m.Name {
				return &machineDeployment, machineList, nil
			}
		}
	}
	err = fmt.Errorf("machine %s is not managed by a MachineDeployment", machineName)
	return nil, nil, err
}

// listMachinesInDeployment lists machines in deployment
func (client *k8sClusterApi) listMachinesInDeployment(ms *clusterv1.MachineDeployment) (*clusterv1.MachineList, error) {
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

type Controller interface {
	checkPreconditions() error
	checkSuccess() error
	executeAction() error
}

// machineDeploymentController executes a MachineDeployment scaling action request.
type machineDeploymentController struct {
	request           *actionRequest               // The action request
	machineDeployment *clusterv1.MachineDeployment // the MachineDeployment controlling the machine
	machineList       *clusterv1.MachineList       // the Machines managed by the MachineDeployment before action execution
}

//
// ------------------------------------------------------------------------------------------------------------------
//

// Check preconditions
func (controller *machineDeploymentController) checkPreconditions() error {
	ok, err := controller.checkMachineDeployment(controller.machineDeployment)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("machine deployment is not in the coherent state")
	}
	// See that we don't drop below 1.
	resultingReplicas := int(*controller.machineDeployment.Spec.Replicas) + int(controller.request.diff)
	if resultingReplicas < 1 {
		return fmt.Errorf("machine deployment replicas can't be brought down to 0")
	}
	return nil
}

// executeAction scales a MachineSet by modifying its replica count
func (controller *machineDeploymentController) executeAction() error {
	desiredReplicas := controller.machineDeployment.Status.Replicas + controller.request.diff
	controller.machineDeployment.Spec.Replicas = &desiredReplicas
	machineDeployment, err := controller.request.client.machineDeployment.Update(controller.machineDeployment)
	if err != nil {
		return err
	}
	controller.machineDeployment = machineDeployment
	return nil
}

// stateCheck checks for a state.
type stateCheck func(...interface{}) (bool, error)

// checkMachineSet checks whether current replica set matches the list of alive machines.
func (controller *machineDeploymentController) checkMachineDeployment(args ...interface{}) (bool, error) {
	machineDeployment := args[0].(*clusterv1.MachineDeployment)
	if machineDeployment.Spec.Replicas == nil {
		return false, fmt.Errorf("MachineDeployment %s invalid replica count (nil)", machineDeployment.Name)
	}
	// get MachineSet's list of managed Machines
	machineList, err := controller.request.client.listMachinesInDeployment(machineDeployment)
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
	if int(*machineDeployment.Spec.Replicas) != alive {
		return false, nil
	}
	return true, nil
}

// identifyDiff locates machine in list1 which is not in list2
func (controller *machineDeploymentController) identifyDiff(list1, list2 *clusterv1.MachineList) *clusterv1.Machine {
	for _, machine1 := range list1.Items {
		for _, machine2 := range list2.Items {
			if machine1.Name != machine2.Name {
				return &machine1
			}
		}
	}
	return nil
}

// checkSuccess verifies that the action has been successful.
func (controller *machineDeploymentController) checkSuccess() error {
	stateDesc := fmt.Sprintf("MachineSet %s contains %d Machines", controller.machineDeployment.Name, *controller.machineDeployment.Spec.Replicas)
	err := controller.waitForState(stateDesc, controller.checkMachineDeployment, controller.machineDeployment)
	if err != nil {
		return err
	}
	// get post-Action list of Machines in the MachineSet
	machineList, err := controller.request.client.listMachinesInDeployment(controller.machineDeployment)
	if err != nil {
		return err
	}
	// Identify the extra machine.
	// Wait for the node provisioning.
	if controller.request.actionType == ProvisionAction {
		newMachine := controller.identifyDiff(machineList, controller.machineList)
		if newMachine == nil {
			return fmt.Errorf("no new machine has been identified for machineDeployment %v", controller.machineDeployment)
		}
		err = controller.waitForNodeProvisioning(newMachine)
	} else {
		oldMachine := controller.identifyDiff(controller.machineList, machineList)
		if oldMachine == nil {
			return nil
		}
		err = controller.waitForNodeDeprovisioning(oldMachine)
	}
	if err != nil {
		return fmt.Errorf("machine failed to provision new machine in machineDeployment %s: %v", controller.machineDeployment.Name, err)
	}
	return nil
}

// checkMachineSuccess checks whether machine has been created successfully.
func (controller *machineDeploymentController) checkMachineSuccess(args ...interface{}) (bool, error) {
	machineName := args[0].(string)
	machine, err := controller.request.client.machine.Get(machineName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if machine.ObjectMeta.CreationTimestamp.String() != "" && machine.Status.ErrorMessage == nil {
		return true, nil
	}
	return false, nil
}

// isNodeReady checks whether the node is ready.
func (controller *machineDeploymentController) isMachineReady(args ...interface{}) (bool, error) {
	machineName := args[0].(string)
	machine, err := controller.request.client.machine.Get(machineName, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	if machine.Status.ErrorMessage != nil {
		return true, nil
	}
	return true, nil
}

// waitForNodeProvisioning waits for the new node to be provisioned with timeout.
func (controller *machineDeploymentController) waitForNodeProvisioning(newMachine *clusterv1.Machine) error {
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
	newNName := newMachine.ObjectMeta.Name
	// wait for new Node to be in Ready state
	descr = fmt.Sprintf("machine %s is Ready", newNName)
	return controller.waitForState(descr, controller.isMachineReady, newNName)
}

// isNodeDeletedOrNotReady checks whether the node is deleted or not ready.
func (controller *machineDeploymentController) isNodeDeletedOrNotReady(args ...interface{}) (bool, error) {
	machineName := args[0].(string)
	machine, err := controller.request.client.machine.Get(machineName, metav1.GetOptions{})
	if err != nil {
		return true, nil
	}
	if machine.ObjectMeta.DeletionTimestamp != nil {
		return true, nil
	}
	return false, nil
}

// waitForNodeDeprovisioning waits for the new node to be de-provisioned with timeout.
func (controller *machineDeploymentController) waitForNodeDeprovisioning(machine *clusterv1.Machine) error {
	deletedNName := machine.Name
	descr := fmt.Sprintf("node %s deleted or exited Ready state", deletedNName)
	return controller.waitForState(descr, controller.isNodeDeletedOrNotReady, deletedNName)
}

// waitForState Is the function that allows to wait for a specific state, or until it times out.
func (controller *machineDeploymentController) waitForState(stateDesc string, f stateCheck, args ...interface{}) error {
	for i := 0; i < operationMaxWaits; i++ {
		ok, err := f(args...)
		if err != nil {
			return err
		}
		// We are done, return
		if ok {
			return nil
		}
		time.Sleep(operationWaitSleepInterval)
	}
	return fmt.Errorf("cannot verify %s: timed out after %v",
		stateDesc, time.Duration(operationMaxWaits)*operationWaitSleepInterval)
}

// IsClusterAPIEnabled checks whether cluster API is in fact enabled.
func IsClusterAPIEnabled(cApiClient *clientset.Clientset, kubeClient *kubernetes.Clientset) (bool, error) {
	if cApiClient == nil {
		return false, nil
	}
	// Construct the API clients.
	client := &k8sClusterApi{
		caClient:          cApiClient,
		k8sClient:         kubeClient,
		discovery:         kubeClient.Discovery(),
		machine:           cApiClient.ClusterV1alpha1().Machines(clusterAPINamespace),
		machineSet:        cApiClient.ClusterV1alpha1().MachineSets(clusterAPINamespace),
		machineDeployment: cApiClient.ClusterV1alpha1().MachineDeployments(clusterAPINamespace),
		caGroupVersion:    clusterAPIGroupVersion,
	}
	// Check whether Cluster API is enabled.
	if err := client.verifyClusterAPIEnabled(); err != nil {
		return false, nil
	}
	return true, nil
}

// Construct the controller
func newController(nodeName string, diff int32, actionType ActionType,
	cApiClient *clientset.Clientset, kubeClient *kubernetes.Clientset) (Controller, *string, error) {
	if cApiClient == nil {
		return nil, nil, fmt.Errorf("no Cluster API available")
	}
	// Construct the API clients.
	client := &k8sClusterApi{
		caClient:          cApiClient,
		k8sClient:         kubeClient,
		discovery:         kubeClient.Discovery(),
		machine:           cApiClient.ClusterV1alpha1().Machines(clusterAPINamespace),
		machineSet:        cApiClient.ClusterV1alpha1().MachineSets(clusterAPINamespace),
		machineDeployment: cApiClient.ClusterV1alpha1().MachineDeployments(clusterAPINamespace),
		caGroupVersion:    clusterAPIGroupVersion,
	}
	// Check whether Cluster API is enabled.
	if err := client.verifyClusterAPIEnabled(); err != nil {
		return nil, nil, fmt.Errorf("cluster API is not enabled for %s: %v", nodeName, err)
	}
	// Identify managing machine.
	machine, err := client.identifyManagingMachine(nodeName)
	if err != nil {
		err = fmt.Errorf("cannot identify machine: %v", err)
		return nil, nil, err
	}
	machineDeployment, mList, err := client.identifyManagingMachineSet(machine.Name)
	if err != nil {
		err = fmt.Errorf("cannot identify machine set: %v", err)
		return nil, nil, err
	}
	request := &actionRequest{client, nodeName, diff, actionType}
	machineDeploymentName := &machineDeployment.Name
	return &machineDeploymentController{request, machineDeployment, mList},
		machineDeploymentName, nil
}
