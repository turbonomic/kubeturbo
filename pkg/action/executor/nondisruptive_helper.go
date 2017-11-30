package executor

import (
	"fmt"
	"github.com/golang/glog"
	autil "github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/util"
	kclient "k8s.io/client-go/kubernetes"
)

// Performs operations for non-disruptive action (container resize and pod move).
// If there is only one child pod for the associated controller, the process is:
//  - update the replicas to 2 (pod p1 created by controller)
//  - perform the action on the target object
//  - delete p1 (pod p2 created by controller)
//  - update the replicas to 1 (p2 deleted by controller)
type NonDisruptiveHelper struct {
	namespace  string
	contKind   string
	contName   string
	targetPod  string
	k8sVersion string
	client     *kclient.Clientset

	podToClean string
}

func NewNonDisruptiveHelper(client *kclient.Clientset, namespace, contKind, contName, targetPod, k8sVersion string) *NonDisruptiveHelper {
	p := &NonDisruptiveHelper{
		namespace:  namespace,
		contKind:   contKind,
		contName:   contName,
		targetPod:  targetPod,
		k8sVersion: k8sVersion,
		client:     client,
	}

	return p
}

// Performs operations before executing a disruptive action (container resize and pod move).
// It will update the controller replicas to 2 and save the new pod created by controller internally
// for cleanup after the action is performed.
func (h *NonDisruptiveHelper) OperateForNonDisruption() error {
	replicas, _, err := autil.GetReplicaStatus(h.client, h.namespace, h.contName, h.contKind)
	if err != nil {
		glog.Errorf("Error getting replica status for %s %s/%s: %s", h.contKind, h.namespace, h.contName, err)
		return fmt.Errorf("failed operations for non-disruptive action")
	}

	if replicas != 1 {
		return nil
	}

	if !h.isOperationSupported() {
		return fmt.Errorf("non-disruptive action is not supported for %s %s/%s", h.contKind, h.namespace, h.contName)
	}

	glog.V(2).Infof("Started non-disruptive operations on %s %s/%s", h.contKind, h.namespace, h.contName)

	// Update replicas to 2
	newReplicas := replicas + 1
	glog.V(4).Infof("Updating replicas to %d", newReplicas)
	if err := h.updateReplicaWithRetry(newReplicas); err != nil {
		glog.Errorf("Error updating replica to 2 for %s %s/%s: %s", h.contKind, h.namespace, h.contName, err)
		return fmt.Errorf("failed operations for non-disruptive action")
	}

	// Get the new pod
	glog.V(4).Infof("Getting the new pod")
	pods, err := autil.FindPodsByController(h.client, h.namespace, h.contName, h.contKind)
	if err != nil {
		glog.Errorf("Error finding pods by %s %s/%s: %s", h.contKind, h.namespace, h.contName, err)
		return fmt.Errorf("failed operations for non-disruptive action")
	}

	// There should be exactly two pods
	if len(pods) != newReplicas {
		glog.Errorf("Found %d (not 2) pods by %s %s/%s", len(pods), h.contKind, h.namespace, h.contName)
		return fmt.Errorf("failed operations for non-disruptive action")
	}

	for _, p1 := range pods {
		if p1.Name != h.targetPod {
			h.podToClean = p1.Name
			break
		}
	}

	glog.V(4).Infof("The pod to clean up: %s", h.podToClean)

	glog.V(2).Infof("Finished non-disruptive operations on %s %s/%s", h.contKind, h.namespace, h.contName)

	return nil
}

// Cleans up the pod created in the operation for non-disruptive action support.
// The pod, if exists, will be deleted and the replicas of the associated controller
// will be set to one.
func (h *NonDisruptiveHelper) CleanUp() {
	if h.podToClean == "" { // No operations for non-disruptive actions
		glog.V(4).Infof("No need to clean up for non-disruptive operations on %s %s/%s", h.contKind, h.namespace, h.contName)
		return
	}

	glog.V(2).Infof("Started clean up for non-disruptive operations on %s %s/%s", h.contKind, h.namespace, h.contName)

	// Wait for at least two pods ready before deleting the podToClean
	glog.V(2).Infof("Wait for the target pod to be ready...")
	err := util.RetryDuring(defaultCheckUpdateReplicaRetry, defaultCheckUpdateReplicaTimeout, defaultCheckUpdateReplicaSleep, func() error {
		ctrlReplicas, ctrlReadyReplicas, err := autil.GetReplicaStatus(h.client, h.namespace, h.contName, h.contKind)
		if err != nil {
			return err
		}

		glog.V(2).Infof("replicas: %d, readyReplicas: %d", ctrlReplicas, ctrlReadyReplicas)

		if ctrlReadyReplicas > 1 {
			return nil
		}

		return fmt.Errorf("the operation for the target pod has not yet finished")
	})

	if err != nil {
		glog.Errorf("Error updating replica to 1 for %s %s/%s: %s", h.contKind, h.namespace, h.contName, err)
	}

	// Delete podToClean
	glog.V(4).Infof("Deleting pod: %s", h.podToClean)
	err = autil.DeletePod(h.client, h.namespace, h.podToClean, podDeletionGracePeriodDefault)

	if err != nil {
		glog.Errorf("Error while deleting pod %s: %s", h.podToClean, err)
	}

	// Set replica = 1
	// Check if the current replica = 2 as our action may take several minutes,
	// user may modify the replicaNum during this period.
	ctrlReplicas, _, err := autil.GetReplicaStatus(h.client, h.namespace, h.contName, h.contKind)
	if err != nil {
		glog.Errorf("Error while getting replica status for %s %s/%s: %s", h.contKind, h.namespace, h.contName, err)
		return
	}

	if ctrlReplicas != 2 {
		glog.Errorf("Clean up terminated: the controller replica is not 2")
		return
	}

	glog.V(4).Infof("Updating replicas to 1")
	if err := h.updateReplicaWithRetry(1); err != nil {
		glog.Errorf("Clean up failed while updating replica to 1 for %s %s/%s: %s", h.contKind, h.namespace, h.contName, err)
		return
	}

	glog.V(2).Infof("Finished clean up for non-disruptive operations on %s %s/%s", h.contKind, h.namespace, h.contName)
}

func (h *NonDisruptiveHelper) updateReplicaWithRetry(newReplicas int) error {
	// With retrying several times
	glog.V(2).Infof("updateReplicaWithRetry: newReplicas = %d", newReplicas)
	err := util.RetryDuring(defaultUpdateReplicaRetry, defaultUpdateReplicaTimeout, defaultUpdateReplicaSleep, func() error {
		return autil.UpdateReplicas(h.client, h.namespace, h.contName, h.contKind, newReplicas)
	})

	if err != nil {
		return err
	}

	// Wait for the update to finish
	glog.V(2).Infof("Wait for the update to finish...")
	err = util.RetryDuring(defaultCheckUpdateReplicaRetry, defaultCheckUpdateReplicaTimeout, defaultCheckUpdateReplicaSleep, func() error {
		ctrlReplicas, ctrlReadyReplicas, err := autil.GetReplicaStatus(h.client, h.namespace, h.contName, h.contKind)
		if err != nil {
			return err
		}

		glog.V(4).Infof("replicas: %d, readyReplicas: %d", ctrlReplicas, ctrlReadyReplicas)

		if ctrlReplicas == newReplicas && ctrlReadyReplicas == newReplicas {
			return nil
		}

		return fmt.Errorf("the replica update has not yet finished")
	})

	if err != nil {
		return err
	}

	return nil
}

func (h *NonDisruptiveHelper) isOperationSupported() bool {
	switch h.contKind {
	case util.KindReplicationController:
		return true
	case util.KindReplicaSet:
		return true
	case util.KindDeployment:
		if util.CompareVersion(h.k8sVersion, HigherK8sVersion) < 0 {
			glog.Errorf("The non-disruptive operation is not supported for Deployment controllers in version %s", h.k8sVersion)
			return false
		}

		return true
	default:
		glog.Errorf("unsupported controller kind: %s", h.contKind)
		return false
	}
}
