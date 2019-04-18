/*
Copyright 2019 Turbonomic, Inc.
*/

package executor

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
	clientcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/typed/cluster/v1alpha1"
)

//
// Constants
//

// vmDtoPropK8sNodeName identifies the VM EntityDTO property containing the name of the Node corresponding to the VM.
// const vmDTOPropK8sNodeName = "KubernetesNodeName"

// clusterAPIGroupVersion is the ClusterAPI group & version supported by this package.
const clusterAPIGroupVersion = "cluster.k8s.io/v1alpha1"

// clusterAPINamespace is the ClusterAPI namespace used that defines all Cluster API system resources.
const clusterAPINamespace = v1.NamespaceDefault

// nodeAnnotationMachine identifies the Node Annotation containing the name of the Machine corresponding to the Node.
const nodeAnnotationMachine = "machine"

// // actionKeepAliveInterval is the frequency of updates sent to the Turbo server to prevent an Action time-out.
// const ActionKeepAliveInterval = 2 * time.Second // 25 * time.Second
//
// // Constants that determine maximum time to wait for scaling action to complete before returning a timeout error
// const (
// 	// Maximum wait time for the MachineSet controller to create a new Machine object or delete an existing Machine.
// 	machineDefinedMaxWaits = 2 // 15
// 	machineDefinedWaitTime = 1 * time.Second // 2 * time.Second
//
// 	// Maximum wait time for the Machine controller to provision a new Node or de-provision an existing Node.
// 	nodeCreatedMaxWaits = 2 // 15
// 	nodeCreatedWaitTime = 1 * time.Second // 2 * time.Second
//
// 	// Maximum wait time for a newly provisioned Node to achieve Ready State.
// 	nodeReadyMaxWaits = 2 // 60
// 	nodeReadyWaitTime = 1 * time.Second // 10 * time.Second
//
// 	// Maximum wait time for a de-provisioned Node to exit Ready State.
// 	nodeNotReadyMaxWaits = 2 // 60
// 	nodeNotReadyWaitTime = 1 * time.Second // 10 * time.Second
// )

// actionKeepAliveInterval is the frequency of updates sent to the Turbo server to prevent an Action time-out.
const ActionKeepAliveInterval = 25 * time.Second

// Constants that determine maximum time to wait for scaling action to complete before returning a timeout error
const (
	// Maximum wait time for the MachineSet controller to create a new Machine object or delete an existing Machine.
	// machineDefinedMaxWaits = 60
	// machineDefinedWaitTime = 10 * time.Second
	machineCreatedMaxWaits = 15
	machineCreatedWaitTime = 2 * time.Second

	// Maximum wait time for the MachineSet controller to create a new Machine object or delete an existing Machine.
	machineDeletedMaxWaits = 30
	machineDeletedWaitTime = 10 * time.Second

	// Maximum wait time for the Machine controller to provision a new Node.
	nodeCreatedMaxWaits = 60
	nodeCreatedWaitTime = 10 * time.Second

	// Maximum wait time for a newly provisioned Node to achieve Ready State.
	nodeReadyMaxWaits = 30
	nodeReadyWaitTime = 10 * time.Second

	// Maximum wait time for a de-provisioned Node to exit Ready State.
	nodeNotReadyMaxWaits = 60
	nodeNotReadyWaitTime = 10 * time.Second
)

//
// Progress Indicator messages, Log levels and Errors
//

// Design: Three dimensions determine levels and messages:
//   o Scope:      Program flow ("Verifying preconditions"), API ("Retrieving Node"), details ("Variable values")
//   o Activity:   Major ("Verifying preconditions"), minor ("Verifying Cluster API enabled"), API ("Retrieving Node")
//   o Life cycle: Begin ("Retrieving MachineSets"), end ("3 MachineSets Retrieved")
//
// Messages: New activity start messages are formulated in present tense.  All other messages are in the past tense.
// Progress Indicator update messages begin new major activities and are, therefore, in the present tense.  Since they
// also depict general program flow, these messages are automatically logged at Level 1.
//
// Log levels: The combination of dimensions support 6 log levels:
//
// Level  "Java"  Scope         Activity        Begin/end  Usage
// -----  ------  ------------  --------------  ---------  -------------------------------
//   1    Info    Program flow  Action phase    Begin      Only used by Progress Indicator
//   2    Debug   Program flow  Phase/activity  End        Major and minor activity
//   3    Trace   Program flow  Minor activity  Begin
//   4    Trace   API           API call        End        Results Summary
//   5    Trace   API           API call        Begin/end  Call summary and result details
//   6    Trace   Program flow                             All details
//
// Errors: While Errors can be returned from any level of code, they are only logged at the very top of the program
// stack before termination.

//
// Locking
//

// Lock design: Since "Node" Scaling is really MachineSet scaling, the affected MachineSet is locked while the Executor
// processes the scaling request to prevent clashes from multiple concurrent Actions.  Multiple Actions may be generated
// on the same Node (if the Action executes across multiple Turbo Market cycles) or on different Nodes that resolve to
// the same MachineSet.
//

//
// Auxiliary types
//

//
// ActionType type
//

// ActionType describes the current phase of processing the Action request.
type ActionType string

// These are the valid Action types.
const (
	// ProvisionAction is an action to provision a new Node.
	ProvisionAction ActionType = "Provision"
	// SuspendAction is an action to suspend an existing Node.
	SuspendAction ActionType = "Suspend"
)

//
// ActionPhase type
//

// ActionPhase describes the current phase of processing the Action request.
type ActionPhase string

// These are the valid Action phases.
const (
	// ActionLockAcquisition is the phase of acquiring a lock for the Scaling action.
	ActionLockAcquisition ActionPhase = "Lock Acquisition"
	// ActionIdentification is the phase of identifying the Scaling Controller that will scale a Node.
	ActionIdentification ActionPhase = "Identification"
	// ActionPrecondition is the phase of verifying preconditions to ensure the action will execute successfully.
	ActionPrecondition ActionPhase = "Precondition"
	// ActionExecution is the phase when the system is actually executing the requested action.
	ActionExecution ActionPhase = "Execution"
	// ActionPostcondition is the phase of verifying postconditions to be sure the action executed successfully.
	ActionPostcondition ActionPhase = "Postcondition"
	// ActionSucceeded means the action completed successfully.
	ActionSucceeded ActionPhase = "Succeeded"
)

//
// ActionError type
//

// The ActionError type encapsulates a run-time error, adding the Action phase in which the error occurred.
type ActionError struct {
	phase ActionPhase // action phase, e.g., "precondition" or "postcondition"
	err   error       // error encountered
}

func (e *ActionError) Error() string {
	return e.err.Error()
}

//
// progress type
//

// progress manages a progress indicator for an action executor and sends progress updates to the Turbo server.
type progress struct {
	logPrefix string      // log message prefix
	pct       int         // percent complete
	desc      string      // description of current progress, e.g., "Verifying Node status"
	phase     ActionPhase // current phase of processing, e.g., "Preconditions"
	action    string      // action being executed, e.g., "Scaling Action"
	tracker   string      // Turbo server progress tracker
	// TODO: replace the tracker with a Turbo tracker when code is integrated with kubeturbo
	// tracker   sdkprobe.ActionProgressTracker // Turbo server progress tracker
}

// NewProgress returns a new progress indicator.
// func NewProgress(logPrefix string, tracker string) *progress {
// 	return &progress{logPrefix, 0, "In progress", "", "Action", tracker}
// }
func NewProgress(tracker string) *progress {
	return &progress{"", 0, "In progress", "", "Action", tracker}
}

// // setLogPrefix sets logPrefix used by the progress indicator in log messages.
// func (p *progress) setLogPrefix(logPrefix string) {
// 	p.logPrefix = logPrefix
// }

// update updates the progress indicator, logs its current status and sends it to the Turbo server.
func (p *progress) Update(pct int, desc string, phaseAction ...interface{}) error {
	return p.update(pct, desc, phaseAction...)
}
func (p *progress) update(pct int, desc string, phaseAction ...interface{}) error {
	p.pct = pct
	p.desc = desc
	if len(phaseAction) > 0 {
		// cast optional object and prevent nil cast panic
		p.phase, _ = phaseAction[0].(ActionPhase)
	}
	if len(phaseAction) > 1 {
		// cast optional object and prevent nil cast panic
		p.action, _ = phaseAction[1].(string)
	}

	// log current status
	glog.Infof("%sProgress: %d%% (%s) %s", p.logPrefix, p.pct, p.phase, p.desc)

	// send progress update to Turbo server
	p.updateServer("progress")

	return nil
}

// updateServer sends a progress indicator update to the Turbo server
func (p *progress) updateServer(source string) {
	if glog.V(6) {
		glog.Infof("%s*** Updating Turbo (%s): %d%% %s", p.logPrefix, source, p.pct, p.desc)
	}

	// TODO: Send progress tracker update to Turbo server when this code is integrated with kubeturbo
	// p.tracker.UpdateProgress(proto.ActionResponseState_IN_PROGRESS, p.desc, p.pct)
}

// keepAlive sends progress updates to the Turbo server at a regular interval to prevent it from timing out the Action.
// It stops sending updates when the stop channel receives input.
func (p *progress) KeepAlive(interval time.Duration, stop chan struct{}) {
	glog.Infof("%sProgress: *** keepAlive goroutine starting", p.logPrefix)
	for {
		glog.Infof("%sProgress: *** keepAlive goroutine pinging Turbo server", p.logPrefix)
		p.updateServer("keepalive")
		t := time.NewTimer(interval)
		select {
		case <-stop:
			glog.Infof("%sProgress: *** keepAlive goroutine exiting", p.logPrefix)
			return
		case <-t.C:
		}
	}
}

type TurboNodeActionExecutor interface {
	TurboActionExecutor
	ExecuteWithProgress(vmDTO *TurboActionExecutorInput, progress *progress, lockKey string) (*TurboActionExecutorOutput, error)
}

//
// ScaleActionExecutor Interface
//

// ScaleActionExecutor implementations horizontally scale a Node.  The interface includes methods to verify
// preconditions for action success, execute the action and verify the action's success.
type ScaleActionExecutor interface {
	TurboNodeActionExecutor

	// parseInput parses an ActionItemDTO and returns a ScalingRequest.
	// parseInput(*proto.ActionItemDTO, *progress) (*scalingRequest, error)
	parseInput(*TurboActionExecutorInput, *progress) (*scalingRequest, error)

	// identifyActionController returns a Controller than can be used to execute the ScalingRequest.
	identifyActionController(*scalingRequest) (ScalingController, error)

	// verifyActionPreconditions verifies scaling action preconditions.
	verifyActionPreconditions(ScalingController) error

	// executeAction executes the scaling action.
	executeAction(ScalingController) error

	// verifyActionSucceeded verifies that the scaling action was successful.
	verifyActionSucceeded(ScalingController) (*TurboActionExecutorOutput, error)

	// ****************** //

	// LogPrefix returns a string prefix used in all log messages.
	ActionType() ActionType

	// Diff returns the direction of scaling: +1 or -1
	Diff() int32

	// Kubernetes clients
	CApiClient() *clientset.Clientset
	KubeClient() *kubernetes.Clientset
}

//
// Functions on ScaleActionExecutor interface
//

// Execute orchestrates a Node scaling action.  This method is required by the TurboActionExecutor interface and is
// called by the ActionHandler.
// func (h *scaler) Execute(input *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
// func (s *scaler) Execute(vmDTO string, tracker string) (string, error) {
func (s *scaler) Execute(vmDTO *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	// create progress indicator
	// TODO: Use actual Turbo tracker object when integrated
	tracker := "replace this string with tracker object when integrated"
	progress := NewProgress(tracker)

	// prevent Turbo Action time-out by sending stream of updates; stop when Action terminates for any reason
	stop := make(chan struct{})
	defer close(stop)
	go progress.KeepAlive(ActionKeepAliveInterval, stop)

	return ScaleNode(s, vmDTO, progress, "")
}

// func (s *Provisioner) Execute(vmDTO string, tracker string) (string, error) {
func (s *Provisioner) Execute(vmDTO *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	// create progress indicator
	// TODO: Use actual Turbo tracker object when integrated
	tracker := "replace this string with tracker object when integrated"
	progress := NewProgress(tracker)

	// prevent Turbo Action time-out by sending stream of updates; stop when Action terminates for any reason
	stop := make(chan struct{})
	defer close(stop)
	go progress.KeepAlive(ActionKeepAliveInterval, stop)

	return ScaleNode(s, vmDTO, progress, "")
}

// func (s *Suspender) Execute(vmDTO string, tracker string) (string, error) {
func (s *Suspender) Execute(vmDTO *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	// create progress indicator
	// TODO: Use actual Turbo tracker object when integrated
	tracker := "replace this string with tracker object when integrated"
	progress := NewProgress(tracker)

	// prevent Turbo Action time-out by sending stream of updates; stop when Action terminates for any reason
	stop := make(chan struct{})
	defer close(stop)
	go progress.KeepAlive(ActionKeepAliveInterval, stop)

	return ScaleNode(s, vmDTO, progress, "")
}

func (s *scaler) ExecuteWithProgress(vmDTO *TurboActionExecutorInput, progress *progress, lockKey string) (*TurboActionExecutorOutput, error) {
	return ScaleNode(s, vmDTO, progress, lockKey)
}

// func (s *Provisioner) Execute(vmDTO string, tracker string) (string, error) {
func (s *Provisioner) ExecuteWithProgress(vmDTO *TurboActionExecutorInput, progress *progress, lockKey string) (*TurboActionExecutorOutput, error) {
	return ScaleNode(s, vmDTO, progress, lockKey)
}

// func (s *Suspender) Execute(vmDTO string, tracker string) (string, error) {
func (s *Suspender) ExecuteWithProgress(vmDTO *TurboActionExecutorInput, progress *progress, lockKey string) (*TurboActionExecutorOutput, error) {
	return ScaleNode(s, vmDTO, progress, lockKey)
}

// getLockKey returns a key suitable for locking a scaling action to the specific ScalingController that manages a Node.
func GetLockKey(s ScaleActionExecutor, actionItemDTO *TurboActionExecutorInput, progress *progress) (string, error) {
	// parse input and create a ScalingRequest
	sRequest, err := s.parseInput(actionItemDTO, progress)
	if err != nil {
		return "", err
	}

	// create a ScalingController to execute the ScalingRequest that is unique to the controller managing the Node
	sController, err := s.identifyActionController(sRequest)
	if err != nil {
		return "", err
	}

	glog.Info(fmt.Sprintf("*** %T: %v: %t", sController, sController, sController))

	// return the key for the ScalingController
	return sController.GetLockKey()
}

// parseInput parses an input DTO and returns a Request object needed for a Scaling Action.
func (s *scaler) parseInput(dto *TurboActionExecutorInput, progress *progress) (*scalingRequest, error) {
	nodeName := dto.ActionItem.GetCurrentSE().GetDisplayName()
	// TODO(future): Parse the DTO for the number of Nodes to scale.
	//               In the meantime, assume all scaling request are for only a single Node.
	numNodes := int32(1)
	// combine information from the ActionExecutor with the input DTO to create a ScalingRequest.
	quantity := numNodes * s.Diff()
	return NewScalingRequest(nodeName, quantity, s.ActionType(), progress, s.CApiClient(), s.KubeClient()), nil
}

// identifyActionController returns a ScalingController than can be used to execute the ScalingRequest.
func (*scaler) identifyActionController(sRequest *scalingRequest) (ScalingController, error) {
	// create a ScalingController to execute the ScalingRequest that is unique to the controller managing the Node
	return NewScalingController(sRequest)
}

// ScaleNode scales a Node by horizontally scaling its controlling MachineSet, logs the final action result (success or
// error) and returns a string summarizing the action when it succeeds or an error on failure.
// func ScaleNode(s ScaleActionExecutor, *TurboActionExecutorInput string) (*TurboActionExecutorOutput, error) {
// func ScaleNode(s ScaleActionExecutor, vmDTO string, tracker string) (string, error) {
func ScaleNode(s ScaleActionExecutor, vmDTO *TurboActionExecutorInput, progress *progress, lockKey string) (*TurboActionExecutorOutput, error) {
	// process action and log any terminating error encountered
	result, err := scaleNode(s, vmDTO, progress, lockKey)
	logPrefix := progress.logPrefix
	if err != nil {
		// add action phase (e.g., Precondition, Execution, Postcondition) to error log
		phase := ""
		if aerr, ok := err.(*ActionError); ok {
			phase = fmt.Sprintf(" (%s Error)", aerr.phase)
		}
		glog.Errorf(logPrefix+"Action failed%s: %v", phase, err)

		return nil, err
	}

	glog.Infof(logPrefix+"Action succeeded: %s", result)
	return result, nil
}

// verifyIsLocked verifies the Scaling Action was properly locked before attempting execution.
func (sc *msScalingController) verifyIsLocked(lockKey string) error {
	// verify controller was locked prior to execution
	if lockKey == "" {
		return fmt.Errorf("action was not locked before attempting execution")
	}

	// verify Node has not changed ScalingControllers since the Action was originally locked
	if lk, err := sc.GetLockKey(); err != nil {
		return err
	} else if lk != lockKey {
		return fmt.Errorf(
			"action is locked to wrong Controller; Node moved from %s to %s", lockKey, lk)
	}

	return nil
}

// scaleNode scales a Node by horizontally scaling its controlling MachineSet.  Its single argument implements the
// ScaleActionExecutor interface and executes an action to provision or suspend a Node.
// func scaleNode(s ScaleActionExecutor, sc ScalingController) (string, error) {
// func scaleNode(s ScaleActionExecutor) (string, error) {
func scaleNode(s ScaleActionExecutor, vmDTO *TurboActionExecutorInput, progress *progress, lockKey string) (*TurboActionExecutorOutput, error) {
	// parse input and get a ScalingRequest
	err := progress.update(0, "Identifying scaling controller managing the Node", ActionIdentification)
	sr, err := s.parseInput(vmDTO, progress)
	if err != nil {
		return nil, &ActionError{ActionIdentification, err}
	}

	// identify the ScalingController that will handle the Request
	sc, err := s.identifyActionController(sr)
	if err != nil {
		return nil, &ActionError{ActionIdentification, err}
	}

	// verify the Node has not moved between ScalingControllers since the Action was locked
	if err := sc.verifyIsLocked(lockKey); err != nil {
		return nil, &ActionError{ActionIdentification, err}
	}

	// verify scaling action preconditions
	err = sc.Progress().update(10, "Verifying scaling action preconditions", ActionPrecondition)
	err = s.verifyActionPreconditions(sc)
	// err := sc.verifyActionPreconditions()
	if err != nil {
		return nil, &ActionError{ActionPrecondition, err}
	}

	// execute scaling action
	err = sc.Progress().update(20, "Executing scaling action", ActionExecution)
	err = s.executeAction(sc)
	// err = sc.executeAction()
	if err != nil {
		return nil, &ActionError{ActionExecution, err}
	}

	// wait for scaling action to complete
	err = sc.Progress().update(60, "Verifying scaling action postconditions", ActionPostcondition)
	if err != nil {
		return nil, err
	}

	result, err := s.verifyActionSucceeded(sc)
	if err != nil {
		return nil, &ActionError{ActionPostcondition, err}
	}

	err = sc.Progress().update(100, "Scaling action succeeded", ActionSucceeded)
	return result, nil
}

//
// Controller interfaces
//

// Controller manages Action execution for a single Action request.
type Controller interface {
	// verifyActionPreconditions verifies preconditions necessary for the action to succeed.
	verifyActionPreconditions() error

	// executeAction executes the action.
	executeAction() error

	// verifyActionSucceeded verifies that the action succeeded.
	verifyActionSucceeded() (*TurboActionExecutorOutput, error)

	// Progress returns a progress indicator that can be updated based on progress of the execution of this request.
	Progress() *progress
}

// LockedController manages Action execution for a single Action request that needs to be locked during execution.
type LockedController interface {
	Controller

	// GetLockKey returns a unique key based on the controller managing the action for this request.
	GetLockKey() (string, error)

	// verifies the lock key matches the Controller.
	verifyIsLocked(string) error
}

// ScalingController scales a Node using a specific controller type, e.g., MachineSet or MachineDeployment.
type ScalingController interface {
	LockedController
}

//
// types
//

// apiClients encapsulates Kubernetes and ClusterAPI clients and interfaces needed for Node scaling.
type apiClients struct {
	// clients
	cApiClient *clientset.Clientset
	kubeClient *kubernetes.Clientset

	// Core API Resource client interfaces
	discInterface discovery.DiscoveryInterface
	nodeInterface clientcorev1.NodeInterface

	// Cluster API Resource client interfaces
	machInterface v1alpha1.MachineInterface
	mSetInterface v1alpha1.MachineSetInterface
	machineDepInt v1alpha1.MachineDeploymentInterface

	cApiGroupVersion string // clusterAPI group and version
}

type ActionRequest interface {
	LogPrefix()
	Progress()

	// identifyScalingController() (interface{}, *clusterv1.MachineList, error)
	identifyController() (Controller, error)
}

// actionRequest represents a single request for action execution.  This is the "base" type for all action requests.
type actionRequest struct {
	*apiClients

	logPrefix string    // prefix for all log messages
	progress  *progress // action progress indicator
}

// scalingRequest contains the information unique to an individual Node scaling action request.
type scalingRequest struct {
	*actionRequest

	nodeName   string // name of the Node to be cloned or deleted
	diff       int32  // number of Nodes to provision (if diff > 0) or suspend (if diff < 0)
	actionType ActionType
}

// scalingController is the "base" type for all scaling controllers
type scalingController struct {
	*scalingRequest
}

//
// ClusterAPI scaling controllers
//

// machineSetScaler executes a MachineSet scaling action request.
// machineDeploymentScaler executes a MachineDeployment scaling action request.
// machineScaler executes a Machine scaling action request.

// msScalingController executes a MachineSet scaling action request.
// It implements the ScalingController interface, enabling Node scaling to be accomplished by scaling its managing
// MachineSet.
type msScalingController struct {
	*scalingController

	ms    *clusterv1.MachineSet  // the MachineSet controlling the Node
	mList *clusterv1.MachineList // the Machines managed by the MachineSet before action execution
}

type msProvisionerController struct {
	*msScalingController
}

type msSuspenderController struct {
	*msScalingController
}

// mdScalingController contains scaling information that is unique to an individual MachineDeployment scaling action request.
// It implements the ScalingController interface, enabling Node scaling to be accomplished by scaling its managing
// MachineDeployment.
type mdScalingController struct {
	*scalingController
	md    *clusterv1.MachineDeployment // the MachineDeployment controlling the Node
	mList *clusterv1.MachineList       // the Machines managed by the MachineDeployment before action execution
}

// mScalingController contains scaling information that is unique to an individual Machine scaling action request.
// It implements the ScalingController interface, enabling Node scaling to be accomplished by scaling its managing
// MachineSet.
type mScalingController struct {
	*scalingController
	m *clusterv1.Machine // the Machine controlling the Node
}

//
// apiClients Constructor
//

// NewApiClients creates a new apiClients object that encapsulates Kubernetes APIServer clients needed for Node scaling.
func NewApiClients(cApiClient *clientset.Clientset, kubeClient *kubernetes.Clientset) *apiClients {
	return &apiClients{
		// clients
		cApiClient: cApiClient,
		kubeClient: kubeClient,

		// Kubernetes Core resource interfaces
		discInterface: kubeClient.Discovery(),
		nodeInterface: kubeClient.CoreV1().Nodes(),

		// ClusterAPI resource interfaces
		machInterface: cApiClient.ClusterV1alpha1().Machines(clusterAPINamespace),
		mSetInterface: cApiClient.ClusterV1alpha1().MachineSets(clusterAPINamespace),
		machineDepInt: cApiClient.ClusterV1alpha1().MachineDeployments(clusterAPINamespace),

		cApiGroupVersion: clusterAPIGroupVersion,
	}
}

//
// scalingRequest Constructor
//

// NewScalingRequest returns a scalingRequest object unique to an individual Scaling Action request.
// func NewScalingRequest (nodeName string, diff int32, actionType ActionType, tracker string, cApiClient *clientset.Clientset, kubeClient *kubernetes.Clientset) *scalingRequest {
// func NewScalingRequest (nodeName string, diff int32, actionType ActionType, progress *progress, cApiClient *clientset.Clientset, kubeClient *kubernetes.Clientset) *scalingRequest {
// func NewScalingRequest (nodeName string, diff int32, actionType ActionType, progress *progress, cApiClient *clientset.Clientset, kubeClient *kubernetes.Clientset, lockKey string) *scalingRequest {
// func NewScalingRequest (nodeName string, diff int32, actionType ActionType, progress *progress, cApiClient *clientset.Clientset, kubeClient *kubernetes.Clientset) *scalingRequest {
func NewScalingRequest(nodeName string, diff int32, actionType ActionType, progress *progress, cApiClient *clientset.Clientset, kubeClient *kubernetes.Clientset) *scalingRequest {
	// nodeName string, diff int32, actionType ActionType, progress *progress, cApiClient *clientset.Clientset, kubeClient *kubernetes.Clientset
	logPrefix := fmt.Sprintf("[%v Node %s] ", actionType, nodeName)
	apiClients := NewApiClients(cApiClient, kubeClient)
	// progress := NewProgress(logPrefix, tracker)

	// use the same log prefix for the progress indicator
	progress.logPrefix = logPrefix

	ar := &actionRequest{
		apiClients: apiClients,
		logPrefix:  logPrefix,
		progress:   progress,
	}

	return &scalingRequest{
		// defining attributes: Node and Action type identification
		nodeName:   nodeName,
		diff:       diff,
		actionType: actionType,

		// runtime attributes
		actionRequest: ar,

		// logPrefix:           logPrefix,
		// apiClients:          apiClients,
		// progress:            progress,

		// lockKey:             lockKey,
	}
}

//
// scalingController Constructor
//

// NewScalingController is a factory function that returns a ScalingController implementation that supports the
// specific ClusterAPI controller type (i.e., MachineSet, MachineDeployment, Machine) managing the Node being scaled.
func (sRequest *scalingRequest) identifyController() (ScalingController, error) {
	return NewScalingController(sRequest)
}

func NewScalingController(sRequest *scalingRequest) (ScalingController, error) {
	errPrefix := "cannot identify controller managing Node"
	actionType := sRequest.actionType

	// identify the controller that manages the Node, e.g., a MachineSet, MachineDeployment or Machine
	controller, mList, err := sRequest.identifyScalingController()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %v", errPrefix, sRequest.nodeName, err)
	}

	// return a ScalingController customized to the controller type
	sc := &scalingController{sRequest}
	switch v := controller.(type) {
	case *clusterv1.MachineSet:
		switch actionType {
		case ProvisionAction:
			msc := &msProvisionerController{&msScalingController{sc, v, mList}}
			return msc, nil
		case SuspendAction:
			msc := &msSuspenderController{&msScalingController{sc, v, mList}}
			return msc, nil
		default:
			return nil, fmt.Errorf("%s %s: Unexpected scaling action type: %v", errPrefix, sRequest.nodeName, actionType)
		}
	case *clusterv1.MachineDeployment:
		return &mdScalingController{sc, v, mList}, nil
	case *clusterv1.Machine:
		return &mScalingController{sc, v,}, nil
	}

	return sc, fmt.Errorf("%s %s: Unexpected Node scaling controller type: %T", errPrefix, sRequest.nodeName, controller)
}

//
// scalingController methods
//

// Progress returns a progress indicator that can be updated based on progress of the execution of this request. This
// method is required by the ScalingController interface.
func (sc *scalingController) Progress() *progress {
	return sc.progress
}

func (sc *scalingController) GetLockKey() (string, error) {
	return "", fmt.Errorf("GetLockKey: scaling support not yet implemented for controller type %T: %t", sc, sc)
}

func (sc *scalingController) verifyIsLocked(string) error {
	return fmt.Errorf("verifyIsLocked: scaling support not yet implemented for controller type %T: %t", sc, sc)
}

func (sc *scalingController) verifyActionPreconditions() error {
	return fmt.Errorf("verifyActionPreconditions: scaling support not yet implemented for controller type %T: %v", sc, sc)
}

func (sc *scalingController) executeAction() error {
	return fmt.Errorf("executeAction: scaling support not yet implemented for controller type %T: %v", sc, sc)
}

func (sc *scalingController) verifyActionSucceeded() (*TurboActionExecutorOutput, error) {
	return nil, fmt.Errorf("verifyActionSucceeded: scaling support not yet implemented controller type %T: %v", sc, sc)
}

// identifyScalingController returns the ClusterAPI controller managing a specific Node, e.g., a MachineSet, a
// MachineDeployment or a standalone Machine.  It also returns the list of Machines managed by the controller.
func (sr *scalingRequest) identifyScalingController() (interface{}, *clusterv1.MachineList, error) {
	nodeName := sr.nodeName
	logPrefix := sr.logPrefix
	cApiGroupVersion := sr.cApiGroupVersion

	// verify Cluster supports Cluster API
	if err := sr.verifyClusterAPIEnabled(cApiGroupVersion, logPrefix); err != nil {
		return nil, nil, err
	}

	// verify Node exists and is managed by a Cluster API Machine
	msg := fmt.Sprintf("Machine managing Node %s", nodeName)
	m, err := sr.identifyManagingMachine(nodeName, msg, logPrefix)
	if err != nil {
		err = fmt.Errorf("cannot identify %s: %v", msg, err)
		return nil, nil, err
	}

	// verify Machine is not a Master (because Cluster API does not currently support multiple Masters)
	// TODO: Remove this check when a future version of Cluster API can scale Masters
	// if isMaster(m) {
	// 	err = fmt.Errorf("Node %s is a Master and cannot be scaled", nodeName)
	// 	return nil, nil, err
	// }

	// TODO: Add support for scaling MachineDeployments and standalone Machines
	// TODO: In the meantime, support only MachineSets

	// verify Machine is managed by a MachineSet and retrieve the list of all Machines in the MachineSet
	msg = fmt.Sprintf("MachineSet managing Machine %s", m.Name)
	ms, mList, err := sr.identifyManagingMachineSet(m.Name, msg, logPrefix)
	if err != nil {
		err = fmt.Errorf("cannot identify %s: %v", msg, err)
		return nil, nil, err
	}

	return ms, mList, nil
}

//
// msScalerConfig methods
//

// lockKey returns the unique name of the MachineSet managing a scaling action.  This method is required by the
// ScalingController interface.
func (sc *msScalingController) GetLockKey() (string, error) {
	if sc.ms == nil {
		return "", fmt.Errorf("cannot create lock key for Node %s; managing MachineSet is missing", sc.nodeName)
	}
	return sc.ms.Name, nil
}

// verifyActionPreconditions verifies preconditions for scaling a Node managed by a MachineSet.  It identifies the
// MachineSet and retrieves its list of managed Machines. This method is required by the ScaleActionExecutor interface.
func (sc *msScalingController) verifyActionPreconditions() error {
	logPrefix := sc.logPrefix
	ms := sc.ms
	mList := sc.mList

	// verify MachineSet's pre-Action Machine count equals its pre-Action replica count. If not, the MachineSet
	// controller will itself create/delete Machines to bring it into compliance and there might be no need for this
	// Turbo Scaling Action, so fail it until the MachineSet is stable.
	//
	// TODO (Future): When a single Turbo Scaling Action can request multiple Nodes, adjust the Action to consider
	// TODO (Future): the pre-Action difference between the Machine count and replica count.  E.g., if the
	// TODO (Future): pre-Action difference is 1 Node and the Action wants to add 3 Nodes, it is "safe" to add 2 now
	// TODO (Future): and let the MachineController automatically add the third on its own.
	//
	// Note: Cluster API automatically replaces deleted Nodes, but not Nodes that are simply "unhealthy" (e.g.,
	// not in a Ready state); an external process must delete the unhealthy Nodes to trigger their replacement
	// by Cluster API.  In the interim, since Turbo only considers Nodes in Ready state, Turbo will scale out
	// the MachineSet if needed to maintain its capacity.
	replicas := *ms.Spec.Replicas
	b, err := sc.isMSMachineCountEqReplicaCount(logPrefix, ms)
	if !b {
		if err == nil {
			err = fmt.Errorf("MachineSet %s is not stable: Machine count (%d) not equal replica count (%d)", ms.Name, len(mList.Items), replicas)
		}
		return err
	}

	// verify Scaling Action will not reduce replica count below 1 (since Turbo cannot start a first Node).
	// TODO: **** Uncomment after test
	if int(replicas+sc.diff) < 1 {
		return fmt.Errorf("cannot set MachineSet %s replica count < 1 (current=%d, diff=%d)", ms.Name, replicas, sc.diff)
	}
	// TODO: **** Uncomment after test

	return nil
}

// verifyActionSucceeded verifies that a MachineSet managing a Node was successfully scaled.
func (sc *msScalingController) verifyActionSucceeded() (*TurboActionExecutorOutput, error) {
	ms := sc.ms
	mList := sc.mList
	logPrefix := sc.logPrefix
	actionType := sc.actionType
	var result *TurboActionExecutorOutput
	switch actionType {
	case ProvisionAction:
		// wait for MachineSet to stabilize, i.e., when number of Machines equals the replica count
		replicas := *ms.Spec.Replicas
		stateDesc := fmt.Sprintf("MachineSet %s contains %d Machines", ms.Name, replicas)
		// err := sc.waitForState(stateDesc, machineDefinedMaxWaits, machineDefinedWaitTime, 60, 70, sc.isMSMachineCountEqReplicaCount, ms)
		err := sc.waitForState(stateDesc, machineCreatedMaxWaits, machineCreatedWaitTime, 60, 70, sc.isMSMachineCountEqReplicaCount, ms)
		// err := sc.waitForState(stateDesc, machineDefinedMaxWaits, machineDefinedWaitTime, 60, 70, sc.isMSMachineCountEqReplicaCount, ms)
		if err != nil {
			return nil, err
		}

		// get post-Action list of Machines in the MachineSet
		newMList, err := sc.listMachinesInSet(ms, logPrefix)
		if err != nil {
			return nil, err
		}

		// identify the newly added Machine

		// TODO: ***** Remove test code
		// newM, err := sc.identifyExtraMachine(mList, newMList, "new Machine in MachineSet " + ms.Name, logPrefix)
		// TODO: ***** Remove test code

		newM, err := sc.identifyExtraMachine(newMList, mList, "new Machine in MachineSet "+ms.Name, logPrefix)
		if err != nil {
			return nil, err
		}

		// wait for new machine to be provisioned
		result, err = sc.waitForNodeProvisioning(newM)
		if err != nil {
			return nil, fmt.Errorf("Machine %s failed to provision new Node in MachineSet %s: %v", newM.Name, ms.Name, err)
		}
	case SuspendAction:
		// wait for MachineSet to stabilize, i.e., when number of Machines equals the replica count
		replicas := *ms.Spec.Replicas
		stateDesc := fmt.Sprintf("MachineSet %s contains %d Machines", ms.Name, replicas)
		// err := sc.waitForState(stateDesc, machineDefinedMaxWaits, machineDefinedWaitTime, 60, 70, sc.isMSMachineCountEqReplicaCount, ms)
		// err := sc.waitForState(stateDesc, machineCreatedMaxWaits, machineCreatedWaitTime, 60, 70, sc.isMSMachineCountEqReplicaCount, ms)
		err := sc.waitForState(stateDesc, machineDeletedMaxWaits, machineDeletedWaitTime, 60, 70, sc.isMSMachineCountEqReplicaCount, ms)
		if err != nil {
			return nil, err
		}

		// get post-Action list of Machines in the MachineSet
		newMList, err := sc.listMachinesInSet(ms, logPrefix)
		if err != nil {
			return nil, err
		}

		// identify the newly deleted Machine
		deletedM, err := sc.identifyExtraMachine(mList, newMList, "deleted Machine in MachineSet "+ms.Name, logPrefix)
		if err != nil {
			return nil, err
		}

		// wait for deleted Machine to result in Node de-provisioning
		result, err = sc.waitForNodeDeprovisioning(deletedM)
		if err != nil {
			return nil, fmt.Errorf("Machine %s failed to deprovision Node %s in MachineSet %s: %v", deletedM.Name, deletedM.Status.NodeRef.Name, ms.Name, err)
		}
	default:
		return nil, fmt.Errorf("Unexpected MachineSet scaling action type: %v", actionType)
	}

	return result, nil
}

// waitForNodeProvisioning waits for a Node to be provisioned by the Machine controller.  It returns an error
// if the provisioning fails or takes too long.
func (sc *msScalingController) waitForNodeProvisioning(newM *clusterv1.Machine) (*TurboActionExecutorOutput, error) {
	logPrefix := sc.logPrefix

	if glog.V(3) {
		glog.Infof(logPrefix+"Identifying Node created by Machine %s", newM.Name)
	}

	// wait for Machine's status to indicate Node creation final status
	stateDesc := &TurboActionExecutorOutput{
		Succeeded: true,
		// TODO: Uncomment the following once Descr support is introduced in TurboActionExecutorOutput
		//Descr:     fmt.Sprintf("Machine %s Node creation status is final", newM.Name),
	}
	descr := fmt.Sprintf("Machine %s Node creation status is final", newM.Name)
	err := sc.waitForState(descr, nodeCreatedMaxWaits, nodeCreatedWaitTime, 70, 90, sc.isNodeSuccessOrError, newM.Name)
	if err != nil {
		return nil, err
	}

	// check Node creation final status
	newM, err = sc.getMachine(newM.Name, logPrefix)
	if err != nil {
		return nil, err;
	}

	if newM.Status.ErrorMessage != nil {
		err = fmt.Errorf("Machine %s failed to create new Node: %v: %s", newM.Name, newM.Status.ErrorReason, newM.Status.ErrorMessage)
		return nil, err
	}
	newNName := newM.Status.NodeRef.Name
	if glog.V(2) {
		glog.Infof(logPrefix+"Identified %s as Node created by Machine %s", newNName, newM.Name)
	}

	// wait for new Node to be in Ready state
	descr = fmt.Sprintf("Node %s is Ready", newNName)
	err = sc.waitForState(descr, nodeReadyMaxWaits, nodeReadyWaitTime, 90, 100, sc.isNodeReady, newNName)
	if err != nil {
		return nil, err
	}

	return stateDesc, nil
}

// waitForNodeDeprovisioning waits for a Node to be de-provisioned by the Machine controller.  It returns an error
// if the de-provisioning fails or takes too long.
func (sc *msScalingController) waitForNodeDeprovisioning(deletedM *clusterv1.Machine) (*TurboActionExecutorOutput, error) {
	logPrefix := sc.logPrefix

	if glog.V(3) {
		glog.Infof(logPrefix+"Identifying Node managed by deleted Machine %s", deletedM.Name)
	}

	// get name of Node that will be deleted
	deletedNName := deletedM.Status.NodeRef.Name
	if glog.V(2) {
		glog.Infof(logPrefix+"Identified %s as Node managed by deleted Machine %s", deletedNName, deletedM.Name)
	}

	// wait for Node to be deleted or exit Ready state
	stateDesc := &TurboActionExecutorOutput{
		Succeeded: true,
		// TODO: Uncomment the following once Descr support is introduced in TurboActionExecutorOutput
		//Descr:     fmt.Sprintf("Node %s deleted or exited Ready state", deletedNName),
	}
	descr := fmt.Sprintf("Node %s deleted or exited Ready state", deletedNName)
	err := sc.waitForState(descr, nodeNotReadyMaxWaits, nodeNotReadyWaitTime, 70, 100, sc.isNodeDeletedOrNotReady, deletedNName)
	if err != nil {
		return nil, err
	}
	return stateDesc, nil
}

// executeAction scales a MachineSet by modifying its replica count. This method is required by the ScaleActionExecutor
// interface.
func (sc *msScalingController) executeAction() error {
	ms := sc.ms
	diff := sc.diff
	logPrefix := sc.logPrefix

	replicas := *ms.Spec.Replicas
	desiredReplicas := replicas + diff
	ms.Spec.Replicas = &desiredReplicas

	// update the MachineSet
	updateMsg := fmt.Sprintf("MachineSet %s replica count from %d to %d", ms.Name, replicas, desiredReplicas)
	err := sc.progress.update(20, "Updating "+updateMsg)
	if err != nil {
		return err
	}
	return sc.updateMachineSet(ms, updateMsg, logPrefix)
}

//
// Implementations of ScaleActionExecutor interface
//

// scaler "scales" a Node by horizontally scaling its controlling Cluster API MachineSet, which then adds or
// deletes a Machine and a corresponding Node.  scaler is the "base type" for Provisioner and Suspender
// "sub-types" that contain additional precondition and postcondition code unique to their use cases.
// scaler and its "subtypes" all implement the ScaleActionExecutor interface.
type scaler struct {
	*apiClients
	diff       int32      // number of Nodes to provision (if diff > 0) or suspend (if diff < 0)
	actionType ActionType // used in log messages, e.g., Provision or Suspend
}

// Provisioner "provisions" a Node by horizontally scaling-out its controlling Cluster API MachineSet.
// It relies on the scaler type for most of its behavior, but adds postcondition verification unique to provisioning.
// It implements the ScaleActionExecutor interface.
type Provisioner struct {
	*scaler
}

// Suspender "suspends" a Node by horizontally scaling-in its controlling Cluster API MachineSet.
// It relies on the scaler type for most of its behavior, but adds precondition and postcondition verification unique
// to deprovisioning.  It implements the ScaleActionExecutor interface.
type Suspender struct {
	*scaler
}

//
// Constructors
//

// NewNodeScaler creates a new scaler object, initializing Cluster API interfaces and the logging prefix.
// func NewNodeScaler(c *clientset.Clientset, kubeClient *kubernetes.Clientset, diff int32, logPrefix string) (*scaler, error) {
func NewNodeScaler(cApiClient *clientset.Clientset, kubeClient *kubernetes.Clientset, diff int32, actionType ActionType) (*scaler, error) {
	apiClients := NewApiClients(cApiClient, kubeClient)
	return &scaler{
		diff:       diff,
		actionType: actionType,
		apiClients: apiClients,
	}, nil
}

// NewNodeProvisioner creates a new Provisioner object, initializing API resource interfaces and the logging prefix.
func NewNodeProvisioner(cApiClient *clientset.Clientset, kubeClient *kubernetes.Clientset) (*Provisioner, error) {
	s, err := NewNodeScaler(cApiClient, kubeClient, +1, ProvisionAction)
	if err != nil {
		return nil, err
	}
	return &Provisioner{s}, nil
}

// NewNodeSuspender creates a new Suspender object, initializing API resource interfaces and the logging prefix.
func NewNodeSuspender(cApiClient *clientset.Clientset, kubeClient *kubernetes.Clientset) (*Suspender, error) {
	s, err := NewNodeScaler(cApiClient, kubeClient, -1, SuspendAction)
	if err != nil {
		return nil, err
	}
	return &Suspender{s}, nil
}

//
// Methods - Provisioner
//

// verifyActionSucceeded waits until a Node Provision action has definitively succeeded or failed.  It returns an error
// if it cannot verify the results within a reasonable amount of time.
// This method is required by the ScaleActionExecutor interface.
func (s *Provisioner) verifyActionSucceeded(sc ScalingController) (*TurboActionExecutorOutput, error) {
	return sc.verifyActionSucceeded()
}

// isNodeSuccessOrError returns true if a Machine's Status includes either a NodeRef (signifying Node provisioning
// success) or an ErrorMsg (signifying Node provisioningfailure).
// Its single argument, a string, is the name of the Machine.  It implements the isState interface.
// func (s *Provisioner) isNodeSuccessOrError(logPrefix string, args ...interface{}) (bool, error) {
func (sc *scalingController) isNodeSuccessOrError(logPrefix string, args ...interface{}) (bool, error) {

	// typecast arg to string (Machine name)
	name := args[0].(string)

	// retrieve Machine
	newM, err := sc.getMachine(name, logPrefix)
	if err != nil {
		return false, err
	}

	// check for NodeRef (success) or Error (failure)
	return (newM.Status.NodeRef != nil || newM.Status.ErrorMessage != nil), nil
}

//
// Methods - Suspender
//

// verifyPreconditions identifies all cluster resources required for scaling, retrieves them and verifies conditions
// necessary for the scaling action to succeed.  This method is required by the ScaleActionExecutor interface.
// This version of the code adds a verification specific to the Node Suspend case.
func (s *Suspender) verifyActionPreconditions(sc ScalingController) error {
	return sc.verifyActionPreconditions()
}

// verifyActionSucceeded waits until a Node Suspend action has succeeded.  It returns an error if it cannot verify
// success within a reasonable amount of time.  ms is the MachineSet and mList is the list of Machines before the action
// was executed. This method is required by the ScaleActionExecutor interface.
func (s *Suspender) verifyActionSucceeded(sc ScalingController) (*TurboActionExecutorOutput, error) {
	return sc.verifyActionSucceeded()
}

//
// Methods - scaler
//

// ActionType returns a prefix suitable for log messages. This method is required by the ScaleActionExecutor interface.
func (s *scaler) ActionType() ActionType {
	return s.actionType
}

func (s *scaler) Diff() int32 {
	return s.diff
}

func (s *scaler) CApiClient() *clientset.Clientset {
	return s.cApiClient
}

func (s *scaler) KubeClient() *kubernetes.Clientset {
	return s.kubeClient
}

// verifyPreconditions identifies all cluster resources required for scaling, retrieves them and verifies conditions
// necessary for the scaling action to succeed.  This method is required by the ScaleActionExecutor interface. When the
// lockKey is present, additional verifications are performed.
func (s *scaler) verifyActionPreconditions(sc ScalingController) error {
	return sc.verifyActionPreconditions()
}

// executeAction modifies a MachineSet's replica count. This method is required by the ScaleActionExecutor interface.
func (s *scaler) executeAction(sc ScalingController) error {
	return sc.executeAction()
}

// verifyActionSucceeded waits until the number of Machines in a MachineSet matches the MachineSet's replica count.
// It returns an error if it cannot verify success within a reasonable amount of time.  ms is the MachineSet and mList
// is the list of Machines before the action was executed.
//
// This method is part of the base scaler type implementation used by both the Provisioner and Suspender types.
// It is required by the ScaleActionExecutor interface.
func (s *scaler) verifyActionSucceeded(sc ScalingController) (*TurboActionExecutorOutput, error) {
	return sc.verifyActionSucceeded()
}

//
// State verification methods (indicating that some phase of a scaling action concluded)
//

// isState is the function type arg to the waitForState method. It takes any number of arguments of any
// type.  It returns a boolean that tells waitForState whether it a state has been attained and it surfaces any
// errors encountered by the actual implementations.
type isState func(string, ...interface{}) (bool, error)

// waitForState runs a function (of type isState) multiple times until the function returns true. Returns an error
// after too many attempts calling the function. progStart and progEnd are the start and end progress indicator
// percentages, respectively
func (sc *scalingController) waitForState(stateDesc string, maxWaits int, waitTime time.Duration, progStart, progEnd int, f isState, args ...interface{}) error {
	progress := sc.progress
	logPrefix := sc.logPrefix

	// calculate progress indicator increment for each wait cycle
	progInc := float32(progEnd-progStart) / float32(maxWaits+1)

	var fErr error
	numWaits := 0
	for {
		err := progress.update(progStart+int(progInc*float32(numWaits)), "Verifying "+stateDesc)
		if err != nil {
			return err
		}

		// call function to check state
		b, err := f(logPrefix, args...)
		if err == nil && b {
			if glog.V(2) {
				glog.Infof(logPrefix+"Verified %s (wait=%v)", stateDesc, time.Duration(numWaits)*waitTime)
			}
			return nil
		}
		// Remember the fErr. 
		if fErr == nil && err != nil {
			fErr = err
		}

		numWaits++
		if numWaits > maxWaits {
			break
		}

		// wait
		if glog.V(6) {
			glog.Infof(logPrefix+"State not yet verified; waiting %v (wait #%d/%d) before trying again",
				waitTime, numWaits, maxWaits)
		}
		time.Sleep(waitTime)
	}

	err := fmt.Errorf("cannot verify %s: timed out after %v", stateDesc, time.Duration(maxWaits)*waitTime)
	if fErr != nil {
		err = fmt.Errorf("%v: %v", err, fErr)
	}

	return err
}

// isNodeReady returns true if the Node's Ready condition is True; returns false otherwise.  Its single argument,
// a string, is the name of the Node.  It implements the isState interface.
func (sc *scalingController) isNodeReady(logPrefix string, args ...interface{}) (bool, error) {
	// typecast arg to string (Node name)
	name := args[0].(string)

	// retrieve Node and check Ready condition
	// node, err := s.getNode(name, logPrefix)
	node, err := sc.getNode(name, logPrefix)
	if err != nil {
		return false, err
	}
	return isNodeReady(node), nil
}

// isNodeDeletedOrNotReady returns true if the Node is deleted or its Ready condition is no longer True; returns false
// otherwise.  Its single argument, a string, is the name of the Node.  It implements the isState interface.
func (sc *scalingController) isNodeDeletedOrNotReady(logPrefix string, args ...interface{}) (bool, error) {
	ready, err := sc.isNodeReady(logPrefix, args...)
	return (err != nil || !ready), nil
}

// isNumMachinesEqMSReplicaCount returns true if the number of Machines currently defined for a MachineSet equals the
// MachineSets's specified replica count. Its single argument is the MachineSet.  It implements the isState
// interface.
func (sc *msScalingController) isMSMachineCountEqReplicaCount(logPrefix string, args ...interface{}) (bool, error) {
	// typecast arg to MachineSet
	ms := args[0].(*clusterv1.MachineSet)

	// get the MachineSet's replica count
	if ms.Spec.Replicas == nil {
		err := fmt.Errorf("MachineSet %s invalid replica count (nil)", ms.Name)
		return false, err
	}
	replicas := *ms.Spec.Replicas

	// get MachineSet's list of managed Machines
	// mList, err := s.listMachinesInSet(ms, logPrefix)
	mList, err := sc.listMachinesInSet(ms, logPrefix)
	if err != nil {
		return false, err
	}

	return int(replicas) == len(mList.Items), nil
}

//
// API Discovery methods
//

// verifyClusterApiEnabled returns an error if the Cluster does not support Cluster API.
func (c *apiClients) verifyClusterAPIEnabled(groupVersion, logPrefix string) error {
	discInterface := c.discInterface

	serviceString := fmt.Sprintf("ClusterAPI service \"%s\"", groupVersion)
	if glog.V(3) {
		glog.Infof(logPrefix+"Verifying %s is available", serviceString)
	}
	// apiResourceList, err := s.kubeClient.Discovery().ServerResourcesForGroupVersion(clusterAPIGroupVersion)
	_, err := discInterface.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		err := fmt.Errorf("%s is not available: %v", serviceString, err)
		return err
	}

	// TODO: Consider checking the returned above (*metav1.APIResourceList) to verify the availability of the
	// TODO: required APIs (Machine, MachineSet) and operations (Get, List, Update).

	if glog.V(2) {
		glog.Infof(logPrefix+"Verified %s is available", serviceString)
	}
	return nil
}

//
// API Get object retrieval methods
//

// getter is a function type passed into the getObjectWithLogging method. It takes the name of an object, retrieves the
// object and returns it as an interface{} type. The general purpose interface{} type is used to enable injection of
// general-purpose logging code into all typed Get functions.
type getter func(string) (interface{}, error)

// getObject runs a function (of type getter) that takes the name of an object and returns the object.  It injects
// logging code before and after the API call:
//   log.V(4) summarizes the retrieved object(s) after the API call completes successfully.
//   log.V(5) precedes the API call and also adds details of the retrieved object(s) after the API call completes.
// Calling functions must cast the returned object to its appropriate type.
func (s *apiClients) getObjectWithLogging(logPrefix string, name string, objectType string, f getter) (interface{}, error) {
	if glog.V(5) {
		glog.Infof(logPrefix+"Retrieving %s %s", objectType, name)
	}

	// call getter function
	n, err := f(name)
	if err != nil {
		err := fmt.Errorf("%s %s not found: %v", objectType, name, err)
		return nil, err
	}

	if glog.V(5) {
		glog.Infof(logPrefix+"Retrieved %s %s: %v", objectType, name, n)
	} else if glog.V(4) {
		glog.Infof(logPrefix+"Retrieved %s %s", objectType, name)
	}
	return n, nil
}

// getNode makes an API call and returns a Node object.
func (s *apiClients) getNode(name, logPrefix string) (*v1.Node, error) {
	o, err := s.getObjectWithLogging(logPrefix, name, "Node",
		func(name string) (interface{}, error) {
			// return s.kubeClient.Nodes().Get(name, metav1.GetOptions{})
			return s.nodeInterface.Get(name, metav1.GetOptions{})
		})

	// cast returned object and prevent nil cast panic
	n, _ := o.(*v1.Node)
	return n, err
}

// getMachine makes an API call and returns a Machine object.
func (s *apiClients) getMachine(name, logPrefix string) (*clusterv1.Machine, error) {
	o, err := s.getObjectWithLogging(logPrefix, name, "Machine",
		func(name string) (interface{}, error) {
			return s.machInterface.Get(name, metav1.GetOptions{})
		})

	// cast returned object and prevent nil cast panic
	m, _ := o.(*clusterv1.Machine)
	return m, err
}

// getMachineSet makes an API call and returns a MachineSet object.
func (s *apiClients) getMachineSet(name, logPrefix string) (*clusterv1.MachineSet, error) {
	o, err := s.getObjectWithLogging(logPrefix, name, "MachineSet",
		func(name string) (interface{}, error) {
			return s.mSetInterface.Get(name, metav1.GetOptions{})
		})

	// cast returned object and prevent nil cast panic
	ms, _ := o.(*clusterv1.MachineSet)
	return ms, err
}

//
// API List objects retrieval methods
//

// lister is a function type passed into the listObjectsWithLogging method.  It takes an object (or its name) and returns
// a list of objects as an interface{} type. The general purpose interface{} type is used to enable injection of
// general-purpose logging code into all typed List functions.
type lister func(interface{}) (lengther, error)

// Implementations of lengther maintain a list of items and provide a single method that returns the list size.
type lengther interface {
	getNumItems() int
}

// machList wraps a MachineList and fulfills the lengther interface.
type machList struct {
	*clusterv1.MachineList
}

// getNumItems returns the number of Machines in a MachineList.
func (l *machList) getNumItems() int {
	return len(l.Items)
}

// machSetList wraps a MachineSetList and fulfills the lengther interface.
type machSetList struct {
	*clusterv1.MachineSetList
}

// getNumItems returns the number of MachineSets in a MachineSetList.
func (l *machSetList) getNumItems() int {
	return len(l.Items)
}

// listObjects runs a function (of type lister) that takes an object and returns a list of objects that meet the
// lengther interface.  It injects logging code before and after the API call:
//   log.V(4) summarizes the retrieved object(s) after the API call completes successfully.
//   log.V(5) precedes the API call and also adds details of the retrieved object(s) after the API call completes.
// Calling functions must cast the returned objects to the appropriate type.
func (s *apiClients) listObjectsWithLogging(logPrefix string, name interface{}, objectType string, f lister) (lengther, error) {
	if glog.V(5) {
		glog.Infof(logPrefix+"Retrieving %s", objectType)
	}

	// call lister function to get list of objects
	l, err := f(name)
	if err != nil {
		err := fmt.Errorf("%s %s not found: %v", objectType, name, err)
		return nil, err
	}

	if glog.V(5) {
		glog.Infof(logPrefix+"Retrieved %d %s: %v", l.getNumItems(), objectType, l)
	} else if glog.V(4) {
		glog.Infof(logPrefix+"Retrieved %d %s", l.getNumItems(), objectType)
	}
	return l, nil
}

// listMachineSetsInCluster makes an API call and returns the list of all MachineSets in the cluster.
func (s *apiClients) listMachineSetsInCluster(logPrefix string) (*clusterv1.MachineSetList, error) {
	rString := fmt.Sprintf("MachineSets in cluster")

	o, err := s.listObjectsWithLogging(logPrefix, nil, rString,
		func(args interface{}) (lengther, error) {
			msList, err := s.mSetInterface.List(metav1.ListOptions{})

			// wrap the list in an object that meets the lengther interface
			return &machSetList{msList}, err
		})

	// cast returned object and prevent nil cast panic
	mList, _ := o.(*machSetList)

	// unwrap and return API object
	return mList.MachineSetList, err
}

// listMachinesInSet makes an API call and returns the list of Machines in a MachineSet, i.e., the list of the
// Machines whose labels match on MachineSet.Spec.Selector.
func (s *apiClients) listMachinesInSet(ms *clusterv1.MachineSet, logPrefix string) (*clusterv1.MachineList, error) {
	sString := metav1.FormatLabelSelector(&ms.Spec.Selector)
	rString := fmt.Sprintf("Machines in MachineSet %s (Selector:\"%v\")", ms.Name, sString)

	o, err := s.listObjectsWithLogging(logPrefix, ms, rString,
		func(arg interface{}) (lengther, error) {
			// typecast arg to MachineSet
			ms := arg.(*clusterv1.MachineSet)

			// get the MachineSet label to use as a Machine selector
			sString := metav1.FormatLabelSelector(&ms.Spec.Selector)
			listOpts := metav1.ListOptions{LabelSelector: sString}

			mList, err := s.machInterface.List(listOpts)

			// wrap the list in an object that meets the lengther interface
			return &machList{mList}, err
		})

	// cast returned object and prevent nil cast panic
	mList, _ := o.(*machList)

	// unwrap and return API object
	return mList.MachineList, err
}

//
// API Update methods
//

func (s *apiClients) updateMachineSet(ms *clusterv1.MachineSet, updateMsg, logPrefix string) error {
	if glog.V(5) {
		glog.Infof(logPrefix + "Updating " + updateMsg)
	}

	// TODO: ***** Uncomment real code below when finished testing
	// ms, err := s.mSetInterface.Update(ms)

	// TODO: ***** Remove test code
	// simulate success
	var err error

	// simulate error
	// err := fmt.Errorf("test update failure")
	// TODO: ***** Remove test code

	if err != nil {
		err = fmt.Errorf("cannot update %s: %v", updateMsg, err)
		return err
	}
	if glog.V(4) {
		glog.Infof(logPrefix+"Updated %s", updateMsg)
	}
	return nil
}

// identifyManagingMachine returns the Machine that manages a Node.  The Machine name is located in a Node annotation.
// An error is returned if the Node, Machine or Node annotation is not found.
func (c *apiClients) identifyManagingMachine(nodeName, msg, logPrefix string) (*clusterv1.Machine, error) {
	if glog.V(3) {
		glog.Infof(logPrefix+"Identifying %s", msg)
	}

	// verify Node exists
	node, err := c.getNode(nodeName, logPrefix)
	if err != nil {
		return nil, err
	}

	// get the Machine name from a Node annotation
	machineName, ok := node.Annotations[nodeAnnotationMachine]
	if !ok {
		return nil, fmt.Errorf("\"%s\" annotation not found on Node %s", nodeAnnotationMachine, nodeName)
	}

	// verify Machine exists
	m, err := c.getMachine(machineName, logPrefix)
	if err != nil {
		return nil, err
	}

	if glog.V(2) {
		glog.Infof(logPrefix+"Identified %s as %s", machineName, msg)
	}
	return m, nil
}

// identifyManagingMachineSet returns the MachineSet that manages a specific Machine and the complete list of Machines
// managed by that MachineSet. Returns an error if the Machine is not managed by a MachineSet.
func (s *apiClients) identifyManagingMachineSet(machineName, msg, logPrefix string) (*clusterv1.MachineSet, *clusterv1.MachineList, error) {
	if glog.V(3) {
		glog.Infof(logPrefix+"Identifying %s", msg)
	}

	// retrieve all MachineSets in the cluster
	msList, err := s.listMachineSetsInCluster(logPrefix)
	if err != nil {
		return nil, nil, err
	}

	// iterate through MachineSets to find the one containing our Machine
	var ms clusterv1.MachineSet
	var mList *clusterv1.MachineList
	msFound := false

FindMSLoop:
	for _, ms = range msList.Items {
		// retrieve all Machines in this MachineSet
		mList, err = s.listMachinesInSet(&ms, logPrefix)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot retrieve Machines in MachineSet %s: %v", ms.Name, err)
		}

		// check for our Machine in the list
		for _, m := range mList.Items {
			if glog.V(6) {
				glog.Infof(logPrefix+"Comparing Machine names %s == %s: %v", machineName, m.Name, machineName == m.Name)
			}
			if machineName == m.Name {
				msFound = true
				if glog.V(6) {
					glog.Infof(logPrefix+"Found Machine %s in MachineSet %s", machineName, ms.Name)
				}
				break FindMSLoop
			}
		}
	}

	if !msFound {
		err = fmt.Errorf("Machine %s is not managed by a MachineSet", ms.Name)
		return nil, nil, err
	}

	if glog.V(2) {
		glog.Infof(logPrefix+"Identified %s as %s", ms.Name, msg)
	}
	return &ms, mList, nil
}

// identifyExtraMachine returns the first Machine in mList1 that is not in mList2, or an error if none is found.
// func (s *scaler) identifyExtraMachine(mList1 *clusterv1.MachineList, mList2 *clusterv1.MachineList, msg, logPrefix string) (*clusterv1.Machine, error) {
func (sc *scalingController) identifyExtraMachine(mList1 *clusterv1.MachineList, mList2 *clusterv1.MachineList, msg, logPrefix string) (*clusterv1.Machine, error) {
	if glog.V(3) {
		glog.Infof(logPrefix+"Identifying %s", msg)
	}
	newM := machineListDifference(mList1, mList2)
	if newM == nil {
		return nil, fmt.Errorf("cannot identify %s", msg)
	}
	if glog.V(2) {
		glog.Infof(logPrefix+"Identified %s as %s", newM.Name, msg)
	}
	return newM, nil
}

//
// Utility functions
//

// func acquireLock(ms *clusterv1.MachineSet) error {
// func acquireLock(key string) error {
func AcquireLock(key string) error {
	glog.Infof("*** Acquiring lock: key=\"%s\"", key)

	// TODO: Use Turbo lock implementation
	glog.Infof("*** Acquired lock: key=\"%s\"", key)
	return nil
}

// func releaseLock(ms *clusterv1.MachineSet) {
// func releaseLock(key string) {
func ReleaseLock(key string) {
	glog.Infof("*** Releasing lock: key=\"%s\"", key)

	// TODO: Use Turbo lock implementation
	glog.Infof("*** Released lock: key=\"%s\"", key)
}

// machineListDifference returns the first Machine in mList1 that is not also in mList2; returns nil if none is found.
func machineListDifference(mList1 *clusterv1.MachineList, mList2 *clusterv1.MachineList) *clusterv1.Machine {
	for _, s1 := range mList1.Items {
		s1Found := false
		for _, s2 := range mList2.Items {
			if s1.Name == s2.Name {
				s1Found = true
				break
			}
		}

		if !s1Found {
			return &s1
		}
	}

	return nil
}

//
// Utility functions copied from: k8s.io/kube-deploy/cluster-api/util/util.go
//

// isNodeReady returns true if the node's NodeReady condition is true; false, otherwise.
func isNodeReady(node *v1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == v1.NodeReady {
			return condition.Status == v1.ConditionTrue
		}
	}

	return false
}
