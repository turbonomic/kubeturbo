package executor

import (
	"fmt"

	"github.com/golang/glog"

	"github.com/turbonomic/kubeturbo/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "k8s.io/client-go/kubernetes"
)

type scaleFunc func() error

type scaleHelper struct {
	client    *kclient.Clientset
	nameSpace string
	podName   string

	//parent controller's kind: ReplicationController/ReplicaSet
	kind string
	//parent controller's name
	controllerName string
	diff           int32

	// update number of Replicas of parent controller
	scale scaleFunc
}

func newScaleHelper(client *kclient.Clientset, nameSpace, podName string) (*scaleHelper, error) {
	p := &scaleHelper{
		client:    client,
		nameSpace: nameSpace,
		podName:   podName,
	}

	return p, nil
}

func (helper *scaleHelper) setParent(kind, name string) error {
	helper.kind = kind
	helper.controllerName = name

	switch kind {
	case util.KindReplicationController:
		helper.scale = helper.scaleRC
	case util.KindReplicaSet:
		helper.scale = helper.scaleRS
	case util.KindDeployment:
		helper.scale = helper.scaleDeployment
	default:
		err := fmt.Errorf("Unsupport ControllerType[%s] for scaling Pod", kind)
		glog.Errorf(err.Error())
		return err
	}

	return nil
}

func (helper *scaleHelper) suspendPod() error {
	podClient := helper.client.CoreV1().Pods(helper.nameSpace)
	if _, err := podClient.Get(helper.podName, metav1.GetOptions{}); err != nil {
		glog.Errorf("Failed to get latest pod %s/%s: %v", helper.nameSpace, helper.podName, err)
		return err
	}
	// This function does not block
	if err := podClient.Delete(helper.podName, &metav1.DeleteOptions{}); err != nil {
		glog.Errorf("Failed to delete pod %s/%s: %v", helper.nameSpace, helper.podName, err)
		return err
	}
	return nil
}

// suspendOrProvision suspends or provisions the target pod and returns the desired replica number
// after suspension or provision
//
// Note: For now we only suspend the target pod
//
// TODO: Provision a new pod on the target host
func (helper *scaleHelper) suspendOrProvision(current int32) (int32, error) {
	//1. validate replica number
	result := current + helper.diff
	glog.V(4).Infof("Current replica %d, diff %d, result %d.", current, helper.diff, result)
	if result < 1 {
		return 0, fmt.Errorf("Resulting replica is less than 1 after suspension")
	}
	//2. suspend the target
	if helper.diff < 0 {
		if err := helper.suspendPod(); err != nil {
			return 0, err
		}
	}
	return result, nil
}

// scaleRC scales a ReplicationController
func (helper *scaleHelper) scaleRC() error {
	rcClient := helper.client.CoreV1().ReplicationControllers(helper.nameSpace)
	fullName := fmt.Sprintf("%s/%s", helper.nameSpace, helper.podName)

	//1. get
	rc, err := rcClient.Get(helper.controllerName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Failed to get ReplicationController for pod %s: %v", fullName, err)
		return err
	}

	//2. suspend or provision the target pod
	num, err := helper.suspendOrProvision(*(rc.Spec.Replicas))
	if err != nil {
		glog.Errorf("Failed to scale ReplicationController for pod %s: %v", fullName, err)
		return err
	}
	rc.Spec.Replicas = &num

	//3. update the desired replica number
	if _, err = rcClient.Update(rc); err != nil {
		glog.Errorf("Failed to update ReplicationController for pod %s: %v", fullName, err)
		return err
	}

	return nil
}

// scaleRS scales a Replicaset
func (helper *scaleHelper) scaleRS() error {
	rsClient := helper.client.ExtensionsV1beta1().ReplicaSets(helper.nameSpace)
	fullName := fmt.Sprintf("%s/%s", helper.nameSpace, helper.podName)

	//1. get it
	rs, err := rsClient.Get(helper.controllerName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Failed to get ReplicaSet for pod %s: %v", fullName, err)
		return err
	}

	//2. suspend or provision the target pod
	num, err := helper.suspendOrProvision(*(rs.Spec.Replicas))
	if err != nil {
		glog.Errorf("Failed to scale ReplicaSet for pod %s: %v", fullName, err)
		return err
	}
	rs.Spec.Replicas = &num

	//3. update the desired replica number
	if _, err = rsClient.Update(rs); err != nil {
		glog.Errorf("Failed to update ReplicaSet for pod %s: %v", fullName, err)
		return err
	}

	return nil
}

// scaleDeployment scales a deployment in the following steps:
// For scale in action:
// 1. Delete the target pod
// 2. Decrease the replicas of the deployment by one
// There can be a very short window of race condition between step 1 and step 2, at which time
// k8s deployment controller spins up a new pod. But as soon as the replica number is decreased,
// the newly started pod will be immediately terminated before any containers can have a chance
// to start.
//
// For scale out action:
// 1. Increase the replicas of the deployment by one
// TODO: In the future, if we know the target host of the pod to be provisioned, we can
// create a pod on the target host, and associate the pod with the deployment
func (helper *scaleHelper) scaleDeployment() error {
	depClient := helper.client.AppsV1beta1().Deployments(helper.nameSpace)
	fullName := fmt.Sprintf("%s/%s", helper.nameSpace, helper.podName)

	//1. get it
	rs, err := depClient.Get(helper.controllerName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Failed to get Deployment for pod %s: %v", fullName, err)
		return err
	}

	//2. suspend or provision the target pod
	num, err := helper.suspendOrProvision(*(rs.Spec.Replicas))
	if err != nil {
		glog.Errorf("Failed to scale Deployment for pod %s: %v", fullName, err)
		return err
	}
	rs.Spec.Replicas = &num

	//3. update the desired replica number
	if _, err = depClient.Update(rs); err != nil {
		glog.Errorf("Failed to update Deployment for pod %s: %v", fullName, err)
		return err
	}

	return nil
}
