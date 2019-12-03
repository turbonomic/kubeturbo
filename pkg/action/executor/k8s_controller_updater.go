package executor

import (
	"fmt"
	"github.com/golang/glog"
	podutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/util"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	kclient "k8s.io/client-go/kubernetes"
	"time"
)

// k8sControllerUpdater defines a specific k8s controller resource
// and the mechanism to obtain and update it
type k8sControllerUpdater struct {
	controller k8sController
	client     *kclient.Clientset
	name       string
	namespace  string
	podName    string
}

// controllerSpec defines the portion of a controller specification that we are interested in
// - replicasDiff: 1 for provision, -1 for suspend
// - resizeSpec: the index and new resource requirement of a container
type controllerSpec struct {
	replicasDiff int32
	resizeSpec   *containerResizeSpec
}

// newK8sControllerUpdater returns a k8sControllerUpdater based on the parent kind of a pod
func newK8sControllerUpdater(client *kclient.Clientset, dynamicClient dynamic.Interface, pod *api.Pod) (*k8sControllerUpdater, error) {
	// Find parent kind of the pod
	kind, name, err := podutil.GetPodGrandInfo(dynamicClient, pod)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent info of pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}

	var res schema.GroupVersionResource
	switch kind {
	case util.KindReplicationController:
		res = schema.GroupVersionResource{
			Group:    util.K8sAPIReplicationControllerGV.Group,
			Version:  util.K8sAPIReplicationControllerGV.Version,
			Resource: util.ReplicationControllerResName}
	case util.KindReplicaSet:
		res = schema.GroupVersionResource{
			Group:    util.K8sAPIDeploymentGV.Group,
			Version:  util.K8sAPIDeploymentGV.Version,
			Resource: util.ReplicaSetResName}
	case util.KindDeployment:
		res = schema.GroupVersionResource{
			Group:    util.K8sAPIReplicasetGV.Group,
			Version:  util.K8sAPIReplicasetGV.Version,
			Resource: util.DeploymentResName}
	default:
		err := fmt.Errorf("unsupport controller type %s for pod %s/%s", kind, pod.Namespace, pod.Name)
		return nil, err
	}
	return &k8sControllerUpdater{
		controller: &parentController{
			dynNamespacedClient: dynamicClient.Resource(res).Namespace(pod.Namespace),
			name:                kind,
		},
		client:    client,
		name:      name,
		namespace: pod.Namespace,
		podName:   pod.Name,
	}, nil
}

// updateWithRetry updates a specific k8s controller with retry and timeout
func (c *k8sControllerUpdater) updateWithRetry(spec *controllerSpec) error {
	retryNum := defaultRetryLess
	interval := defaultUpdateReplicaSleep
	timeout := time.Duration(retryNum+1) * interval
	err := util.RetryDuring(retryNum, timeout, interval, func() error {
		return c.update(spec)
	})
	return err
}

// update updates a specific k8s controller in the following steps:
// - Get and save the current controller object from the server
// - Reconcile the current specification with the desired specification, and
//   update the current specification in place if the changes are valid
// - Update the controller object with the server after a successful reconciliation
func (c *k8sControllerUpdater) update(desired *controllerSpec) error {
	glog.V(4).Infof("Begin to update %v of pod %s/%s",
		c.controller, c.namespace, c.podName)
	current, err := c.controller.get(c.name)
	if err != nil {
		return err
	}
	updated, err := c.reconcile(current, desired)
	if err != nil {
		return err
	}
	if !updated {
		glog.V(2).Infof("%v of pod %s/%s has already been updated to the desired specification",
			c.controller, c.namespace, c.podName)
		return nil
	}
	if err := c.controller.update(current); err != nil {
		return err
	}
	glog.V(2).Infof("Successfully updated %v of pod %s/%s",
		c.controller, c.namespace, c.podName)
	return nil
}

// reconcile validates the desired specification and updates the saved controller object in place
// Nothing is updated if there is no change needed. This can happen when resize actions
// from multiple pods that belong to the same controller are generated
func (c *k8sControllerUpdater) reconcile(current *k8sControllerSpec, desired *controllerSpec) (bool, error) {
	if desired.replicasDiff != 0 {
		// This is a horizontal scale
		// We want to suspend the target pod or provision a new pod first
		num, err := c.suspendOrProvision(*current.replicas, desired.replicasDiff)
		if err != nil {
			return false, err
		}
		// Update the replicas of the controller
		glog.V(2).Infof("Try to update replicas of %v from %d to %d",
			c.controller, *current.replicas, num)
		*current.replicas = num
		return true, nil
	}
	// This may be a vertical scale
	// Check and update resource limits/requests of the container in the pod specification
	glog.V(4).Infof("Update container %v/%v-%v resources in the pod specification.",
		c.namespace, c.podName, desired.resizeSpec.Index)
	updated, err := updateResourceAmount(current.podSpec, desired.resizeSpec)
	if err != nil {
		return false, err
	}
	return updated, nil
}

// suspendOrProvision suspends or provisions the target pod and
// returns the desired replica number after suspension or provision
// Note: For now we only suspend the target pod
// TODO: Provision a new pod on the target host
func (c *k8sControllerUpdater) suspendOrProvision(current, diff int32) (int32, error) {
	//1. validate replica number
	result := current + diff
	glog.V(4).Infof("Current replica %d, diff %d, result %d.", current, diff, result)
	if result < 1 {
		return 0, fmt.Errorf("resulting replica is less than 1 after suspension")
	}
	//2. suspend the target
	if diff < 0 {
		if err := c.suspendPod(); err != nil {
			return 0, err
		}
	}
	return result, nil
}

// suspendPod takes the name of a pod and deletes it
// The call does not block
func (c *k8sControllerUpdater) suspendPod() error {
	podClient := c.client.CoreV1().Pods(c.namespace)
	if _, err := podClient.Get(c.podName, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("failed to get latest pod %s/%s: %v", c.namespace, c.podName, err)
	}
	// This function does not block
	if err := podClient.Delete(c.podName, &metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete pod %s/%s: %v", c.namespace, c.podName, err)
	}
	glog.V(2).Infof("Successfully suspended pod %s/%s", c.namespace, c.podName)
	return nil
}
