package action

import (
	"fmt"
	"time"

	"github.com/turbonomic/kubeturbo/pkg/action/util"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	podutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	api "k8s.io/api/core/v1"
)

type IActionLockStore interface {
	getLock(actionItem *proto.ActionItemDTO) (*util.LockHelper, error)
}

type ActionLockStore struct {
	// The lock map for concurrent control of action execution
	lockMap *util.ExpirationMap

	// The function to get the related pod from action item
	podFunc func(ai *proto.ActionItemDTO) (*api.Pod, error)
}

func newActionLockStore(lockMap *util.ExpirationMap, podFunc func(ai *proto.ActionItemDTO) (*api.Pod, error)) *ActionLockStore {
	return &ActionLockStore{lockMap, podFunc}
}

const (
	defaultWaitLockTimeOut = time.Second * 300
	defaultWaitLockSleep   = time.Second * 10
)

// Acquires the lock for the action item. It will wait and retry if the lock is not available, i.e.,
// the lock is used by other action item.
// The key used to acquire the lock is as follows:
//
// 1. If the action item is associated to a container pod (meaning, the function podFunc returns a pod),
//    the key is the "container name" + "image name" for bare-pod cases and its parent controller id for non-bare-pod cases.
//
// 2. Otherwise, the key is the id of the target SE of the action item.
func (a *ActionLockStore) getLock(actionItem *proto.ActionItemDTO) (*util.LockHelper, error) {
	id := actionItem.GetUuid()
	if key, err := a.getLockKey(actionItem); err != nil {
		return nil, err
	} else {
		glog.V(4).Infof("Action %s: getting lock with key %s", id, key)
		lock, err := a.getLockHelper(key)
		if err != nil {
			glog.Errorf("Action %s: failed to get lock with key %s", id, key)
			return nil, err
		}

		return lock, nil
	}
}

// Gets the lock helper by the given key. It will wait and retry if the lock is not available.
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

// Gets the lock key for the action item.
func (a *ActionLockStore) getLockKey(actionItem *proto.ActionItemDTO) (string, error) {
	pod, err := a.podFunc(actionItem)
	if err != nil {
		return "", err
	}
	if pod == nil {
		// If the pod is nil, simply returning the id of the target SE.
		// Currently, for the actions supported by kubeturbo, there is no such use case for nil pod.
		return actionItem.GetTargetSE().GetId(), nil
	} else {
		return getPodLockKey(pod)
	}
}

// Gets lock key for the pod. For a bare pod, the key is its (first) container name + image name. Otherwise, the key is the formatted string:
// [parentKind]-[pod.Namespace]/[parentName] with its parent controller.
func getPodLockKey(pod *api.Pod) (string, error) {
	ownerInfo, err := podutil.GetPodParentInfo(pod)
	if err != nil {
		glog.Errorf("Failed to get pod[%s] parent info: %v", util.BuildIdentifier(pod.Namespace, pod.Name), err)
		return "", err
	}
	if podutil.IsOwnerInfoEmpty(ownerInfo) {
		// For bare-pod case, the pod name and uid will change after actions applied. So, it's not safe to use as key.
		// Here, use the (first) container name + the image name as the key to safely lock the pod.
		key := pod.Spec.Containers[0].Image + pod.Spec.Containers[0].Name
		return key, nil
	}
	// If the pod is not a bare pod, the key is the parent name.
	return fmt.Sprintf("%v-%v/%v", ownerInfo.Kind, pod.Namespace, ownerInfo.Name), nil
}
