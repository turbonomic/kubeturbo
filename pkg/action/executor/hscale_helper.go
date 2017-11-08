package executor

import (
	"fmt"
	"github.com/golang/glog"
	"time"

	autil "github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "k8s.io/client-go/kubernetes"
)

type updateReplicaNumFunc func(client *kclient.Clientset, nameSpace, name string, diff int32) error

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
	updateReplicaNum updateReplicaNumFunc

	//concurrent control lock.map
	locker *autil.LockHelper
	key    string
}

func NewScaleHelper(client *kclient.Clientset, nameSpace, podName string) (*scaleHelper, error) {
	p := &scaleHelper{
		client:    client,
		nameSpace: nameSpace,
		podName:   podName,
	}

	return p, nil
}

func (helper *scaleHelper) SetParent(kind, name string) error {
	helper.kind = kind
	helper.controllerName = name

	switch kind {
	case util.KindReplicationController:
		helper.updateReplicaNum = updateRCReplicaNum
	case util.KindReplicaSet:
		helper.updateReplicaNum = updateRSReplicaNum
	case util.KindDeployment:
		helper.updateReplicaNum = updateDeploymentReplicaNum
	default:
		err := fmt.Errorf("Unsupport ControllerType[%s] for scaling Pod.", kind)
		glog.Errorf(err.Error())
		return err
	}

	return nil
}

func (helper *scaleHelper) SetupLock(emap *autil.ExpirationMap) error {
	helper.key = fmt.Sprintf("%s-%s-%s", helper.kind, helper.nameSpace, helper.controllerName)
	if emap.GetTTL() < time.Second*2 {
		err := fmt.Errorf("TTL of concurrent control map should be larger than 2 seconds.")
		glog.Error(err.Error())
		return err
	}
	helper.locker, _ = autil.NewLockHelper(helper.key, emap)
	return nil
}

func (helper *scaleHelper) Acquirelock() bool {
	return helper.locker.AcquireLock(func(obj interface{}) {
		helper.lockCallBack()
	})
}

func (helper *scaleHelper) lockCallBack() {
	// do nothing except a log
	glog.V(3).Infof("lockCallBack--Expired lock for pod[%s], parent[%s]", helper.podName, helper.controllerName)
	return
}

func (helper *scaleHelper) Releaselock() {
	// will stop renew in locker.Releaselock
	helper.locker.ReleaseLock()
	glog.V(3).Infof("Released lock for pod[%s], parent[%s]",
		helper.podName, helper.controllerName)
}

// update the lock to prevent timeout
func (helper *scaleHelper) Renewlock() bool {
	return helper.locker.RenewLock()
}

func (h *scaleHelper) KeepRenewLock() {
	h.locker.KeepRenewLock()
}

func (h *scaleHelper) CleanUp() {
	h.Releaselock()
}

//------------------------------------------------------------
func setNum(current, diff int32) (int32, error) {
	if current < 1 && diff < 0 {
		return 0, fmt.Errorf("replica num cannot be less than 0.")
	}

	result := current + diff
	if result < 0 {
		result = 0
	}

	return result, nil
}

// update the number of pod replicas for ReplicationController
func updateRCReplicaNum(client *kclient.Clientset, namespace, name string, diff int32) error {
	rcClient := client.CoreV1().ReplicationControllers(namespace)

	//1. get
	fullName := fmt.Sprintf("%s/%s", namespace, name)
	getOption := metav1.GetOptions{}
	rc, err := rcClient.Get(name, getOption)
	if err != nil {
		glog.Errorf("Failed to get ReplicationController: %s: %v", fullName, err)
		return err
	}

	//2. modify it
	num, err := setNum(*(rc.Spec.Replicas), diff)
	if err != nil {
		glog.Warningf("RC-%s resulting replica num[%v] less than 0. (diff=%v)", fullName, num, diff)
		return fmt.Errorf("Aborted")
	}
	rc.Spec.Replicas = &num

	//3. update it
	_, err = rcClient.Update(rc)
	if err != nil {
		glog.Errorf("Failed to update ReplicationController[%s]: %v", fullName, err)
		return fmt.Errorf("Failed")
	}

	return nil
}

// update the number of pod replicas for ReplicaSet
func updateRSReplicaNum(client *kclient.Clientset, namespace, name string, diff int32) error {
	rsClient := client.ExtensionsV1beta1().ReplicaSets(namespace)

	//1. get it
	fullName := fmt.Sprintf("%s/%s", namespace, name)
	getOption := metav1.GetOptions{}
	rs, err := rsClient.Get(name, getOption)
	if err != nil {
		glog.Errorf("Failed to get ReplicaSet: %s: %v", fullName, err)
		return err
	}

	//2. modify it
	num, err := setNum(*(rs.Spec.Replicas), diff)
	if err != nil {
		glog.Warningf("RS-%s resulting replica num[%v] less than 0. (diff=%v)", fullName, num, diff)
		return fmt.Errorf("Aborted")
	}
	rs.Spec.Replicas = &num

	//3. update it
	_, err = rsClient.Update(rs)
	if err != nil {
		glog.Errorf("Failed to update ReplicaSet[%s]: %v", fullName, err)
		return fmt.Errorf("Failed")
	}

	return nil
}

// update the number of pod replicas for Deployment
func updateDeploymentReplicaNum(client *kclient.Clientset, namespace, name string, diff int32) error {
	depClient := client.AppsV1beta1().Deployments(namespace)

	//1. get it
	fullName := fmt.Sprintf("%s/%s", namespace, name)
	getOption := metav1.GetOptions{}
	rs, err := depClient.Get(name, getOption)
	if err != nil {
		glog.Errorf("Failed to get Deployment: %s: %v", fullName, err)
		return err
	}

	//2. modify it
	num, err := setNum(*(rs.Spec.Replicas), diff)
	if err != nil {
		glog.Warningf("RS-%s resulting replica num[%v] less than 0. (diff=%v)", fullName, num, diff)
		return fmt.Errorf("Aborted")
	}
	rs.Spec.Replicas = &num

	//3. update it
	_, err = depClient.Update(rs)
	if err != nil {
		glog.Errorf("Failed to update Deployment[%s]: %v", fullName, err)
		return fmt.Errorf("Failed")
	}

	return nil
}
