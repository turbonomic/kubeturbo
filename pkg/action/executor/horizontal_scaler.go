package executor

import (
	"fmt"
	"time"
	"github.com/golang/glog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/action/turboaction"
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	dutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	goutil "github.com/turbonomic/kubeturbo/pkg/util"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

var (
	getOption = metav1.GetOptions{}
)

type HorizontalScaler struct {
	kubeClient *kclient.Clientset
	lockmap *util.ExpirationMap
}

func NewHorizontalScaler(client *kclient.Clientset, lmap *util.ExpirationMap) *HorizontalScaler {
	return &HorizontalScaler {
		kubeClient: client,
		lockmap: lmap,
	}
}

func (h *HorizontalScaler) Execute(actionItem *proto.ActionItemDTO) (*turboaction.TurboAction, error) {
	//1. check
	if err := h.preActionCheck(actionItem); err != nil {
		glog.Errorf("check action failed, abort action:%++v", actionItem)
		return nil, err
	}

	//2. prepare
	helper, err := h.prepareHelper(actionItem)
	if err != nil {
		glog.Errorf("Failed to prepare action:%v, abort action %++v", err, actionItem)
		return nil, fmt.Errorf("aborted")
	}

	if err = h.do(helper); err != nil {
		glog.Errorf("Failed to execute action: %v, abort action %++v", err, actionItem)
		return nil, err
	}

	return nil, nil
}

func (h *HorizontalScaler) preActionCheck(action *proto.ActionItemDTO) error {
	if action == nil {
		return fmt.Errorf("ActionItem is nil")
	}

	targetSE := action.GetTargetSE()
	targetEntityType := targetSE.GetEntityType()
	if targetEntityType != proto.EntityDTO_CONTAINER_POD && targetEntityType != proto.EntityDTO_APPLICATION {
		msg := fmt.Sprintf("The target type[%v] for scaling action is neither a Pod nor an Application.", targetEntityType.String())
		glog.Errorf(msg)
		return fmt.Errorf("unsupported target type")
	}
	return nil
}

func (h *HorizontalScaler) prepareHelper(action *proto.ActionItemDTO) (*scaleHelper, error) {
	//1. get pod
	pod, err := h.getProviderPod_hard(action)
	if err != nil {
		glog.Errorf("Failed to find pod: %v", err)
		return nil, err
	}
	helper, _ := NewScaleHelper(h.kubeClient, pod.Namespace, pod.Name)

	//2. get replica diff
	diff, err := h.getReplicaDiff(action)
	if err != nil {
		glog.Errorf("Failed to get ReplicaDiff: %v", err)
		return nil, err
	}
	helper.diff = diff

	//3. find parent info
	parentKind, parentName, err := util.ParseParentInfo(pod)
	if err != nil {
		glog.Errorf("Failed to get parent info for pod: %s/%s", pod.Namespace, pod.Name)
		return nil, err
	}
	if err = helper.SetParent(parentKind, parentName); err != nil {
		return nil, err
	}

	//4. set lock info
	if err = helper.SetMap(h.lockmap); err != nil {
		glog.Errorf("Failed to set lock: %v", err)
		return nil, err
	}

	return helper, nil
}

func (h *HorizontalScaler) getReplicaDiff(action *proto.ActionItemDTO) (int32, error) {
	atype := action.GetActionType()
	if atype == proto.ActionItemDTO_PROVISION {
		// Scale out, increase the replica. diff = 1.
		return 1, nil
	} else if atype == proto.ActionItemDTO_MOVE {
		// TODO, unbind action is send as MOVE. This requires server side change.
		// Scale in, decrease the replica. diff = -1.
		return -1, nil
	} else {
		err := fmt.Errorf("Action[%v] is not a scaling action.", atype.String())
		glog.Errorf(err.Error())
		return 0, err
	}
}

func (h * HorizontalScaler) do(helper *scaleHelper) error {
	fullName := fmt.Sprintf("%s-%s/%s", helper.kind, helper.nameSpace, helper.controllerName)

	//1. get lock for parentController
	timeout := defaultWaitLockTimeOut
	interval := defaultWaitLockSleep
	err := goutil.RetryDuring(1000, timeout, interval, func() error {
		if !helper.Acquirelock() {
			return fmt.Errorf("TryLayer")
		}
		return nil
	})
	if err != nil {
		glog.Errorf("scaleControler[%s] failed: failed to acquire lock.", fullName)
		return err
	}
	defer helper.CleanUp()
	helper.KeepRenewLock()

	//2. update replica number
	retryNum := defaultRetryLess
	interval = defaultUpdateReplicaSleep
	timeout = time.Duration(retryNum + 1) * interval
	err = goutil.RetryDuring(retryNum, timeout, interval, func() error {
		inerr := helper.updateReplicaNum(h.kubeClient, helper.nameSpace, helper.controllerName, helper.diff)
		if inerr != nil {
			glog.Errorf("[%s] failed to update replica num: %v", fullName, inerr)
			return fmt.Errorf("Failed")
		}
		return inerr
	})

	//4. check result
	if err = h.checkResult(helper); err != nil {
		return fmt.Errorf("check Failed.")
	}
	return nil
}

func (h *HorizontalScaler) checkResult(helper *scaleHelper) error {
	// do nothing
	return nil
}

// getProviderPod, "hard" means that we will use brute-force method to find the Pod by UUID
// TODO: build a indexed.cache of the Pods by UUID
func (h *HorizontalScaler) getProviderPod_hard(action *proto.ActionItemDTO) (*api.Pod, error) {
	targetType := action.GetTargetSE().GetEntityType()
	sId := action.GetTargetSE().GetId()
	glog.V(3).Infof("Horizontal-Scale target type is %v", targetType.String())

	podId := sId
	var err error = nil

	switch (targetType) {
	case proto.EntityDTO_CONTAINER_POD:
		podId = sId
	case proto.EntityDTO_CONTAINER:
		containerId := sId
		podId, _, err = dutil.ParseContainerId(containerId)
		if err != nil {
			glog.Errorf("Failed to get podId from containerId[%s]: %v", containerId, err)
			return nil, fmt.Errorf("Failed to parse containerId.")
		}
	case proto.EntityDTO_APPLICATION:
		appId := sId
		podId, err = dutil.PodIdFromApp(appId)
		if err != nil {
			glog.Errorf("Failed to get podId from appId[%s]: %v", appId, err)
			return nil, fmt.Errorf("Failed to parse appId.")
		}
	case proto.EntityDTO_VIRTUAL_APPLICATION:
		currentSE := action.GetCurrentSE()
		seType := currentSE.GetEntityType()
		if seType != proto.EntityDTO_APPLICATION {
			err := fmt.Errorf("Unexpected Entity Type[%v] Vs. EntityDTO_APPLICATION", seType.String())
			glog.Errorf(err.Error())
			return nil, err
		}
		appId := currentSE.GetId()
		podId, err = dutil.PodIdFromApp(appId)
		if err != nil {
			glog.Errorf("Failed to get podId from appId[%s]: %v", appId, err)
			return nil, fmt.Errorf("Failed to parse appId.")
		}
	default:
		err = fmt.Errorf("Illegal target type [%v] for horizontal scaling.", targetType.String())
		glog.Errorf(err.Error())
		return nil, err
	}

	if err != nil {
		return nil, err
	}

	return util.GetPodFromUUID(h.kubeClient, podId)
}


// getProviderPod, "easy" means that we will get Pod info from Entity properties
// TODO: ask sdk-team to fix the bug, the entity properties are missing in the action.
func (h *HorizontalScaler) getProviderPod_easy(action *proto.ActionItemDTO) (*api.Pod, error) {
	targetType := action.GetTargetSE().GetEntityType()
	glog.V(3).Infof("Horizontal-Scale target type is %v", targetType.String())

	//1. get Entity Properties
	var properties  []*proto.EntityDTO_EntityProperty
	switch (targetType) {
	case proto.EntityDTO_CONTAINER_POD:
		properties = action.GetTargetSE().GetEntityProperties()
	case proto.EntityDTO_CONTAINER:
		properties = action.GetTargetSE().GetEntityProperties()
	case proto.EntityDTO_APPLICATION:
		properties = action.GetTargetSE().GetEntityProperties()
	case proto.EntityDTO_VIRTUAL_APPLICATION:
		currentSE := action.GetCurrentSE()
		seType := currentSE.GetEntityType()
		if seType != proto.EntityDTO_APPLICATION {
			err := fmt.Errorf("Unexpected Entity Type[%v] Vs. EntityDTO_APPLICATION", seType.String())
			glog.Errorf(err.Error())
			return nil, err
		}
		properties = currentSE.GetEntityProperties()
	default:
		err := fmt.Errorf("Illegal target type [%v] for horizontal scaling.", targetType.String())
		glog.Errorf(err.Error())
		return nil, err
	}

	//2. get podInfo from Entity Properties
	nameSpace, podName, err := property.GetPodInfoFromProperty(properties)
	if err != nil {
			glog.Errorf("Failed to get podInfo from appEntity (%v), properties: %++v", err, properties)
			return nil, fmt.Errorf("Failed to find PodInfo")
	}

	//3. get Pod from k8s.API
	pod, err := h.kubeClient.CoreV1().Pods(nameSpace).Get(podName, getOption)
	if err != nil {
		glog.Errorf("Failed to get Pod(%s/%s): %v", nameSpace, podName, err)
		return nil, fmt.Errorf("Failed to get Pod from k8sAPI.")
	}
	return pod, nil
}

// ----------------------- for scaleHelper ---------------------------------
//TODO: use the latest lock helper once PR106 is merged
// update the number of pod replicas for ReplicationController
// return (retry, error)
func updateRCReplicaNum(client *kclient.Clientset, namespace, name string, diff int32) error {
	rcClient := client.CoreV1().ReplicationControllers(namespace)

	//1. get
	fullName := fmt.Sprintf("%s/%s", namespace, name)
	rc, err := rcClient.Get(name, getOption)
	if err != nil {
		glog.Errorf("Failed to get ReplicationController: %s: %v", fullName, err)
		return err
	}

	//2. modify it
	num := *(rc.Spec.Replicas) + diff
	if num < 1 {
		glog.Warningf("RC-%s resulting replica num[%v] less than 1. (diff=%v)", fullName, num, diff)
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
// return (retry, error)
func updateRSReplicaNum(client *kclient.Clientset, namespace, name string, diff int32) error {
	rsClient := client.ExtensionsV1beta1().ReplicaSets(namespace)

	//1. get it
	fullName := fmt.Sprintf("%s/%s", namespace, name)
	rs, err := rsClient.Get(name, getOption)
	if err != nil {
		glog.Errorf("Failed to get ReplicaSet: %s: %v", fullName, err)
		return err
	}

	//2. modify it
	num := *(rs.Spec.Replicas) + diff
	if num < 1 {
		glog.Warningf("RC-%s resulting replica num[%v] less than 1. (diff=%v)", fullName, num, diff)
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

type updateReplicaNumFunc func(client *kclient.Clientset, nameSpace, name string, diff int32) error

type scaleHelper struct {
	client           *kclient.Clientset
	nameSpace        string
	podName          string

	//parent controller's kind: ReplicationController/ReplicaSet
	kind             string
	//parent controller's name
	controllerName   string
	diff             int32

	// update number of Replicas of parent controller
	updateReplicaNum updateReplicaNumFunc

	//concurrent control lock.map
	emap *util.ExpirationMap
	key string
	version int64

	//stop Renewing
	stop chan struct{}
	isRenewing bool
}

func NewScaleHelper(client *kclient.Clientset, nameSpace, podName string) (*scaleHelper, error) {
	p := &scaleHelper{
		client: client,
		nameSpace: nameSpace,
		podName: podName,
	}

	return p, nil
}

func (helper *scaleHelper) SetParent(kind, name string) error {
	helper.kind = kind
	helper.controllerName = name

	switch kind {
	case kindReplicationController:
		helper.updateReplicaNum = updateRCReplicaNum
	case kindReplicaSet:
		helper.updateReplicaNum = updateRSReplicaNum
	default:
		err := fmt.Errorf("Unsupport ControllerType[%s] for scaling Pod.", kind)
		glog.Errorf(err.Error())
		return err
	}

	return nil
}

func (helper *scaleHelper)  SetMap(emap *util.ExpirationMap) error {
	helper.emap = emap
	helper.key = fmt.Sprintf("%s-%s-%s", helper.kind, helper.nameSpace, helper.controllerName)
	if emap.GetTTL() < time.Second*2 {
		err := fmt.Errorf("TTL of concurrent control map should be larger than 2 seconds.")
		glog.Error(err)
		return err
	}
	return nil
}

func (helper *scaleHelper) Acquirelock() bool {
	version, flag := helper.emap.Add(helper.key, nil, func(obj interface{}) {
	})

	if !flag {
		glog.V(3).Infof("Failed to get lock for contoller [%s]", helper.controllerName)
		return false
	}

	glog.V(3).Infof("Get lock for pod[%s], parent[%s]", helper.podName, helper.controllerName)
	helper.version = version
	return true
}

func (helper *scaleHelper) Releaselock() {
	helper.emap.Del(helper.key, helper.version)
	glog.V(3).Infof("Released lock for pod[%s], parent[%s]",
		helper.podName, helper.controllerName)
}

// update the lock to prevent timeout
func (h *scaleHelper) Renewlock() bool {
	return h.emap.Touch(h.key, h.version)
}

func (h *scaleHelper) KeepRenewLock() {
	ttl := h.emap.GetTTL()
	interval := ttl / 2
	if interval < time.Second {
		interval = time.Second
	}
	h.isRenewing = true

	go func() {
		for {
			select {
			case <-h.stop:
				glog.V(2).Infof("schedulerHelper stop renewlock.")
				return
			default:
				h.Renewlock()
				time.Sleep(interval)
				glog.V(3).Infof("schedulerHelper renewlock.")
			}
		}
	}()
}

func (h *scaleHelper) StopRenew() {
	if h.isRenewing {
		h.isRenewing = false
		close(h.stop)
	}
}

func (h *scaleHelper) CleanUp() {
	h.Releaselock()
	h.StopRenew()
}
