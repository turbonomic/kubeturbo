package executor

import (
	"fmt"
	"github.com/golang/glog"
	podutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/util"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "k8s.io/client-go/kubernetes"
	"time"
)

type k8sControllerUpdater struct {
	controller k8sController
	client     *kclient.Clientset
	name       string
	namespace  string
	podName    string
}

// controllerSpec defines the portion of a controller specification that we are interested in
type controllerSpec struct {
	replicasDiff int32
}

func newK8sControllerUpdater(client *kclient.Clientset, pod *api.Pod) (*k8sControllerUpdater, error) {
	// Find parent kind of the pod
	kind, name, err := podutil.GetPodGrandInfo(client, pod)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent info for pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}
	var controller k8sController
	switch kind {
	case util.KindReplicationController:
		controller = &replicationController{
			client: client.CoreV1().ReplicationControllers(pod.Namespace),
		}
	case util.KindReplicaSet:
		controller = &replicaSet{
			client: client.ExtensionsV1beta1().ReplicaSets(pod.Namespace),
		}
	case util.KindDeployment:
		controller = &deployment{
			client: client.AppsV1beta1().Deployments(pod.Namespace),
		}
	default:
		err := fmt.Errorf("unsupport controller type %s for pod %s/%s", kind, pod.Namespace, pod.Name)
		return nil, err
	}
	return &k8sControllerUpdater{
		controller: controller,
		client:     client,
		name:       name,
		namespace:  pod.Namespace,
		podName:    pod.Name,
	}, nil
}

func (c *k8sControllerUpdater) updateWithRetry(spec *controllerSpec) error {
	retryNum := defaultRetryLess
	interval := defaultUpdateReplicaSleep
	timeout := time.Duration(retryNum+1) * interval
	err := util.RetryDuring(retryNum, timeout, interval, func() error {
		return c.update(spec)
	})
	return err
}

func (c *k8sControllerUpdater) update(desired *controllerSpec) error {
	current, err := c.controller.get(c.name)
	if err != nil {
		return err
	}
	updated, err := c.reconcile(current, desired)
	if err != nil {
		return err
	}
	if !updated {
		glog.V(2).Infof("%v for pod %s/%s has already been updated to the desired value",
			c.controller, c.namespace, c.podName)
		return nil
	}
	if err := c.controller.update(); err != nil {
		return err
	}
	glog.V(2).Infof("Successfully updated %v for pod %s/%s", c.controller, c.namespace, c.podName)
	return nil
}

func (c *k8sControllerUpdater) reconcile(current *k8sControllerSpec, desired *controllerSpec) (bool, error) {
	if desired.replicasDiff != 0 {
		// This is a horizontal scale
		// We want to suspend the target pod or provision a new pod first
		num, err := c.suspendOrProvision(*current.replicas, desired.replicasDiff)
		if err != nil {
			return false, err
		}
		// Update the replicas of the controller
		*current.replicas = num
		return true, nil
	}
	return false, fmt.Errorf("not supported")
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
