package action

import (
	"fmt"
	"time"

	"github.com/turbonomic/kubeturbo/pkg/action/util"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	api "k8s.io/client-go/pkg/api/v1"
)

type IActionLockStore interface {
	getLock(actionItem *proto.ActionItemDTO) (*util.LockHelper, error)
}

type ActionLockStore struct {
	// The lock map for concurrent control of action execution
	lockMap *util.ExpirationMap

	// The function to get the related pod from action item
	podFunc func(ai *proto.ActionItemDTO) *api.Pod
}

func newActionLockStore(lockMap *util.ExpirationMap, podFunc func(ai *proto.ActionItemDTO) *api.Pod) *ActionLockStore {
	return &ActionLockStore{lockMap, podFunc}
}

const (
	defaultWaitLockTimeOut = time.Second * 300
	defaultWaitLockSleep   = time.Second * 10
)

func (a *ActionLockStore) getLock(actionItem *proto.ActionItemDTO) (*util.LockHelper, error) {
	id := actionItem.GetUuid()
	if key, err := a.getLockKey(actionItem); err != nil {
		return nil, err
	} else {
		glog.V(4).Infof("==Action %s: getting lock with key %s", id, key)
		lock, err := a.getLockHelper(key)
		if err != nil {
			glog.Errorf("==Action %s: failed to get lock with key %s", id, key)
			return nil, err
		}

		return lock, nil
	}
}

func (a *ActionLockStore) getLockHelper(key string) (*util.LockHelper, error) {
	//1. set up lock helper
	helper, err := util.NewLockHelper(key, a.lockMap)
	if err != nil {
		glog.Errorf("Failed to get a lockHelper: %v", err)
		return nil, err
	}

	// 2. wait to get a lock of current Pod
	err = helper.Trylock(defaultWaitLockTimeOut, defaultWaitLockSleep)
	if err != nil {
		glog.Errorf("Failed to acquire lock with key(%v): %v", key, err)
		return nil, err
	}
	return helper, nil
}

func (a *ActionLockStore) getLockKey(ai *proto.ActionItemDTO) (string, error) {
	pod := a.podFunc(ai)

	if pod == nil {
		return ai.GetTargetSE().GetId(), nil
	} else {
		return getPodLockKey(pod)
	}
}

func getPodLockKey(pod *api.Pod) (string, error) {
	// If the pod is a bare pod, the key is the pod id. Otherwise, the key is the parent name.
	if parentKind, parentName, err := util.GetPodParentInfo(pod); err != nil {
		glog.Errorf("Failed to get pod[%s] parent info: %v", util.BuildIdentifier(pod.Namespace, pod.Name), err)
		return "", err
	} else if parentKind != "" { // Not a bare pod
		return fmt.Sprintf("%v-%v/%v", parentKind, pod.Namespace, parentName), nil
	}

	return string(pod.UID), nil
}
