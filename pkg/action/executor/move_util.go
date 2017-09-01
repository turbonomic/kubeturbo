package executor

import (
	"fmt"
	"time"

	"github.com/golang/glog"

	actionUtil "github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"
)

//TODO: check which fields should be copied
func copyPodInfo(oldPod, newPod *api.Pod) {
	//1. typeMeta
	newPod.TypeMeta = oldPod.TypeMeta

	//2. objectMeta
	newPod.ObjectMeta = oldPod.ObjectMeta
	newPod.SelfLink = ""
	newPod.ResourceVersion = ""
	newPod.Generation = 0
	newPod.CreationTimestamp = metav1.Time{}
	newPod.DeletionTimestamp = nil
	newPod.DeletionGracePeriodSeconds = nil

	//3. podSpec
	spec := oldPod.Spec
	spec.Hostname = ""
	spec.Subdomain = ""
	spec.NodeName = ""

	newPod.Spec = spec
	return
}

func calcGracePeriod(pod *api.Pod) int64 {
	grace := podDeletionGracePeriodDefault
	if pod.Spec.TerminationGracePeriodSeconds != nil {
		grace = *(pod.Spec.TerminationGracePeriodSeconds)
		if grace > podDeletionGracePeriodMax {
			grace = podDeletionGracePeriodMax
		}
	}
	return grace
}

// move pod nameSpace/podName to node nodeName
func movePod(client *kclient.Clientset, pod *api.Pod, nodeName string, retryNum int) (*api.Pod, error) {
	podClient := client.CoreV1().Pods(pod.Namespace)
	if podClient == nil {
		err := fmt.Errorf("cannot get Pod client for nameSpace:%v", pod.Namespace)
		glog.Error(err)
		return nil, err
	}

	//1. copy the original pod
	id := fmt.Sprintf("%v/%v", pod.Namespace, pod.Name)
	glog.V(2).Infof("move-pod: begin to move %v from %v to %v",
		id, pod.Spec.NodeName, nodeName)

	npod := &api.Pod{}
	copyPodInfo(pod, npod)
	npod.Spec.NodeName = nodeName

	//2. kill original pod
	grace := calcGracePeriod(pod)
	delOption := &metav1.DeleteOptions{GracePeriodSeconds: &grace}
	err := podClient.Delete(pod.Name, delOption)
	if err != nil {
		err = fmt.Errorf("move-failed: failed to delete original pod-%v: %v",
			id, err)
		glog.Error(err)
		return nil, err
	}

	//3. create (and bind) the new Pod
	time.Sleep(time.Duration(grace) * time.Second + defaultMoreGrace) //wait for the previous pod to be cleaned up.
	interval := defaultPodCreateSleep
	timeout := interval * time.Duration(retryNum) + time.Second * 10
	err = util.RetryDuring(retryNum, timeout, interval, func() error {
		_, inerr := podClient.Create(npod)
		return inerr
	})
	if err != nil {
		err = fmt.Errorf("move-failed: failed to create new pod-%v: %v",
			id, err)
		glog.Error(err)
		return nil, err
	}

	glog.V(2).Infof("move-finished: %v from %v to %v",
		id, pod.Spec.NodeName, nodeName)

	return npod, nil
}

//---------------Move Helper---------------

type getSchedulerNameFunc func(client *kclient.Clientset, nameSpace, name string) (string, error)
type updateSchedulerFunc func(client *kclient.Clientset, nameSpace, name, scheduler string) (string, error)

type moveHelper struct {
	client    *kclient.Clientset
	nameSpace string
	podName   string

	//parent controller's kind: ReplicationController/ReplicaSet
	kind string
	//parent controller's name
	controllerName string

	//the none-exist scheduler name
	schedulerNone string

	//the original scheduler of the parent controller
	scheduler string
	flag      bool

	//functions to manipulate schedulerName via K8s'API
	getSchedulerName    getSchedulerNameFunc
	updateSchedulerName updateSchedulerFunc

	//for the expirationMap
	emap    *actionUtil.ExpirationMap
	key     string
	version int64

	//stop Renewing
	stop chan struct{}
	isRenewing bool
}

func NewMoveHelper(client *kclient.Clientset, nameSpace, name, kind, parentName, noneScheduler string, highver bool) (*moveHelper, error) {

	p := &moveHelper{
		client:         client,
		nameSpace:      nameSpace,
		podName:        name,
		kind:           kind,
		controllerName: parentName,
		schedulerNone:  noneScheduler,
		flag:           false,
		stop: make(chan struct{}),
	}

	switch p.kind {
	case kindReplicationController:
		p.getSchedulerName = actionUtil.GetRCschedulerName
		p.updateSchedulerName = actionUtil.UpdateRCscheduler
		if !highver {
			p.getSchedulerName = actionUtil.GetRCschedulerName15
			p.updateSchedulerName = actionUtil.UpdateRCscheduler15
		}
	case kindReplicaSet:
		p.getSchedulerName = actionUtil.GetRSschedulerName
		p.updateSchedulerName = actionUtil.UpdateRSscheduler
		if !highver {
			p.getSchedulerName = actionUtil.GetRSschedulerName15
			p.updateSchedulerName = actionUtil.UpdateRSscheduler15
		}
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}

	return p, nil
}

func (h *moveHelper) SetMap(emap *actionUtil.ExpirationMap) error {
	h.emap = emap
	h.key = fmt.Sprintf("%s-%s-%s", h.kind, h.nameSpace, h.controllerName)
	if emap.GetTTL() < time.Second * 2 {
		err := fmt.Errorf("TTL of concurrent control map should be larger than 2 seconds.")
		glog.Error(err)
		return err
	}
	return nil
}

// check whether the current scheduler is equal to the expected scheduler.
// will renew lock.
func (h *moveHelper) CheckScheduler(expectedScheduler string, retry int) (bool, error) {

	flag := false

	interval := defaultCheckSchedulerSleep
	timeout := time.Duration(retry) * interval + time.Second * 10
	err := util.RetryDuring(retry, timeout, interval, func() error {
		if flag = h.Renewlock(); !flag {
			glog.Warningf("failed to renew lock to updateScheduler pod[%s], parent[%s].", h.podName, h.controllerName)
			return nil
		}

		scheduler, err := h.getSchedulerName(h.client, h.nameSpace, h.controllerName)
		if err == nil && scheduler == expectedScheduler {
			flag = true
			return nil
		}

		return err
	})

	if err != nil {
		glog.Errorf("failed to check scheduler name for %s: %v", h.key, err)
	}

	return flag, err
}

// need to renew lock
func (h *moveHelper) UpdateScheduler(schedulerName string, retry int) (string, error) {
	result := ""
	flag := true

	interval := defaultUpdateSchedulerSleep
	timeout := time.Duration(retry) * interval + time.Second * 10
	err := util.RetryDuring(retry, timeout, interval, func() error {
		if flag = h.Renewlock(); !flag {
			glog.Warningf("failed to renew lock to updateScheduler pod[%s], parent[%s].", h.podName, h.controllerName)
			return nil
		}

		sname, err := h.updateSchedulerName(h.client, h.nameSpace, h.controllerName, schedulerName)
		result = sname
		return err
	})

	if !flag {
		return result, fmt.Errorf("Timeout")
	}

	if err != nil {
		glog.Error(err)
	}

	return result, err
}

func (h *moveHelper) SetScheduler(schedulerName string) {
	if h.flag {
		glog.Warningf("schedulerName has already been set.")
	}

	h.scheduler = schedulerName
	h.flag = true
}

// CleanUp: (1) restore scheduler Name, (2) Release lock
func (h *moveHelper) CleanUp() {
	defer h.Releaselock()
	defer h.StopRenew()

	if !(h.flag) {
		return
	}

	if flag, _ := h.CheckScheduler(h.schedulerNone, defaultRetryLess); !flag {
		return
	}

	_, err := h.UpdateScheduler(h.scheduler, defaultRetryMore)
	if err != nil {
		glog.Errorf("failed to cleanUp (restoreScheduler) for Pod[%s], parent[%s]", h.podName, h.controllerName)
	}
}

// acquire a lock before manipulate the scheduler of the parentController
func (h *moveHelper) Acquirelock() bool {
	version, flag := h.emap.Add(h.key, nil, func(obj interface{}) {
		h.lockCallBack()
	})

	if !flag {
		glog.V(3).Infof("Failed to get lock for pod[%s], parent[%s]", h.podName, h.controllerName)
		return false
	}

	glog.V(3).Infof("Get lock for pod[%s], parent[%s]", h.podName, h.controllerName)
	h.version = version
	return true
}

// update the lock to prevent timeout
func (h *moveHelper) Renewlock() bool {
	return h.emap.Touch(h.key, h.version)
}

// release the lock of the parentController
func (h *moveHelper) Releaselock() {
	h.emap.Del(h.key, h.version)
	glog.V(3).Infof("Released lock for pod[%s], parent[%s]", h.podName, h.controllerName)
}

// the call back function, the lock should have already be acquired;
// This callback function should do the minimum thing: restore the original scheduler
// the pending pods should be deleted by other things.
func (h *moveHelper) lockCallBack() {
	glog.V(3).Infof("lockCallBack--Expired lock for pod[%s], parent[%s]", h.podName, h.controllerName)
	// check whether need to do reset scheduler
	if !(h.flag) {
		return
	}

	// check whether the scheduler has been changed.
	scheduler, err := h.getSchedulerName(h.client, h.nameSpace, h.controllerName)
	if err != nil || scheduler != h.schedulerNone {
		return
	}

	// restore the original scheduler
	interval := defaultUpdateSchedulerSleep
	timeout := time.Duration(defaultRetryMore) * interval + time.Second * 10
	util.RetryDuring(defaultRetryMore, timeout, interval, func() error {
		_, err := h.updateSchedulerName(h.client, h.nameSpace, h.controllerName, h.scheduler)
		return err
	})

	return
}

func (h *moveHelper) KeepRenewLock() {
	ttl := h.emap.GetTTL()
	interval := ttl/2
	if interval < time.Second {
		interval = time.Second
	}
	h.isRenewing = true

	go func() {
		for {
			select {
			case <- h.stop:
				glog.V(2).Infof("moveHelper stop renewlock.")
				return
			default:
				h.Renewlock()
				time.Sleep(interval)
				glog.V(3).Infof("moveHelper renewlock.")
			}
		}
	}()
}

func (h *moveHelper) StopRenew() {
	if h.isRenewing {
		h.isRenewing = false
		close(h.stop)
	}
}
