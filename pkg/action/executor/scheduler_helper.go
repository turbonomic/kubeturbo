package executor

import (
	"fmt"
	"github.com/golang/glog"
	"time"

	autil "github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/util"

	kclient "k8s.io/client-go/kubernetes"
)

type getSchedulerNameFunc func(client *kclient.Clientset, nameSpace, name string) (string, error)
type updateSchedulerFunc func(client *kclient.Clientset, nameSpace, name, scheduler string) (string, error)

type schedulerHelper struct {
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
	locker *autil.LockHelper
	key    string
}

func NewSchedulerHelper(client *kclient.Clientset, nameSpace, name, kind, parentName, noneScheduler string, highver bool) (*schedulerHelper, error) {

	p := &schedulerHelper{
		client:         client,
		nameSpace:      nameSpace,
		podName:        name,
		kind:           kind,
		controllerName: parentName,
		schedulerNone:  noneScheduler,
		flag:           false,
		//stop:           make(chan struct{}),
	}

	switch p.kind {
	case util.KindReplicationController:
		p.getSchedulerName = autil.GetRCschedulerName
		p.updateSchedulerName = autil.UpdateRCscheduler
		if !highver {
			p.getSchedulerName = autil.GetRCschedulerName15
			p.updateSchedulerName = autil.UpdateRCscheduler15
		}
	case util.KindReplicaSet:
		p.getSchedulerName = autil.GetRSschedulerName
		p.updateSchedulerName = autil.UpdateRSscheduler
		if !highver {
			p.getSchedulerName = autil.GetRSschedulerName15
			p.updateSchedulerName = autil.UpdateRSscheduler15
		}
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}

	return p, nil
}

func (h *schedulerHelper) SetupLock(emap *autil.ExpirationMap) error {
	h.key = fmt.Sprintf("%s-%s-%s", h.kind, h.nameSpace, h.controllerName)
	if emap.GetTTL() < time.Second*2 {
		err := fmt.Errorf("TTL of concurrent control map should be larger than 2 seconds.")
		glog.Error(err)
		return err
	}
	h.locker, _ = autil.NewLockHelper(h.key, emap)
	return nil
}

// check whether the current scheduler is equal to the expected scheduler.
// will renew lock.
func (h *schedulerHelper) CheckScheduler(expectedScheduler string, retry int) (bool, error) {

	flag := false

	interval := defaultCheckSchedulerSleep
	timeout := time.Duration(retry+1) * interval
	err := util.RetryDuring(retry, timeout, interval, func() error {
		if flag = h.locker.RenewLock(); !flag {
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
func (h *schedulerHelper) UpdateScheduler(schedulerName string, retry int) (string, error) {
	result := ""
	flag := true

	interval := defaultUpdateSchedulerSleep
	timeout := time.Duration(retry+1) * interval
	err := util.RetryDuring(retry, timeout, interval, func() error {
		if flag = h.locker.RenewLock(); !flag {
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

func (h *schedulerHelper) SetScheduler(schedulerName string) {
	if h.flag {
		glog.Warningf("schedulerName has already been set.")
	}

	h.scheduler = schedulerName
	h.flag = true
}

// CleanUp: (1) restore scheduler Name, (2) Release lock
func (h *schedulerHelper) CleanUp() {
	defer func() {
		h.locker.ReleaseLock()
	}()

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
func (h *schedulerHelper) Acquirelock() bool {
	return h.locker.AcquireLock(func(obj interface{}) {
		h.lockCallBack()
	})
}

// the call back function, the lock should have already be acquired;
// This callback function should do the minimum thing: restore the original scheduler
// the pending pods should be deleted by other things.
func (h *schedulerHelper) lockCallBack() {
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
	timeout := time.Duration(defaultRetryMore+1) * interval
	util.RetryDuring(defaultRetryMore, timeout, interval, func() error {
		_, err := h.updateSchedulerName(h.client, h.nameSpace, h.controllerName, h.scheduler)
		return err
	})

	return
}

func (h *schedulerHelper) KeepRenewLock() {
	h.locker.KeepRenewLock()
}

func (h *schedulerHelper) StopRenew() {
	h.locker.StopRenew()
}
