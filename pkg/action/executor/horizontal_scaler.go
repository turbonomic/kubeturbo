package executor

import (
	"fmt"
	"github.com/golang/glog"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	dutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	goutil "github.com/turbonomic/kubeturbo/pkg/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type HorizontalScaler struct {
	kubeClient *kclient.Clientset
	lockmap    *util.ExpirationMap
}

func NewHorizontalScaler(client *kclient.Clientset, lmap *util.ExpirationMap) *HorizontalScaler {
	return &HorizontalScaler{
		kubeClient: client,
		lockmap:    lmap,
	}
}

func (h *HorizontalScaler) Execute(actionItem *proto.ActionItemDTO) error {
	//1. check
	if err := h.preActionCheck(actionItem); err != nil {
		glog.Errorf("check action failed, abort action:%++v", actionItem)
		return fmt.Errorf("Failed")
	}

	//2. prepare
	helper, err := h.prepareHelper(actionItem)
	if err != nil {
		glog.Errorf("Failed to prepare action:%v, abort action %++v", err, actionItem)
		return fmt.Errorf("Aborted")
	}

	//3. execute the action
	if err = h.do(helper); err != nil {
		glog.Errorf("Failed to execute action: %v, abort action %++v", err, actionItem)
		return fmt.Errorf("Failed")
	}

	//4. check action result
	glog.V(2).Infof("Begin to check action resulf of HorizontalScale for pod[%v]", helper.key)
	if err = h.checkResult(helper); err != nil {
		glog.Errorf("HorizontalScale checking failed: %v", err)
		return fmt.Errorf("Failed")
	}
	glog.V(2).Infof("Action HorizontalScale for pod[%v] succeeded.", helper.key)

	return nil
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
	parentKind, parentName, err := util.GetPodGrandInfo(h.kubeClient, pod)
	if err != nil {
		glog.Errorf("Failed to get parent info for pod: %s/%s", pod.Namespace, pod.Name)
		return nil, err
	}
	if err = helper.SetParent(parentKind, parentName); err != nil {
		return nil, err
	}

	//4. set lock info
	if err = helper.SetupLock(h.lockmap); err != nil {
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

func (h *HorizontalScaler) do(helper *scaleHelper) error {
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
	timeout = time.Duration(retryNum+1) * interval
	err = goutil.RetryDuring(retryNum, timeout, interval, func() error {
		inerr := helper.updateReplicaNum(h.kubeClient, helper.nameSpace, helper.controllerName, helper.diff)
		if inerr != nil {
			glog.Errorf("[%s] failed to update replica num: %v", fullName, inerr)
			return fmt.Errorf("Failed")
		}
		return inerr
	})

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
	displayName := action.GetTargetSE().GetDisplayName()
	var err error = nil

	switch targetType {
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

		// get container's DisplayName
		displayName = action.GetHostedBySE().GetDisplayName()
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
		displayName = dutil.GetPodFullNameFromAppName(currentSE.GetDisplayName())
	default:
		err = fmt.Errorf("Illegal target type [%v] for horizontal scaling.", targetType.String())
		glog.Errorf(err.Error())
		return nil, err
	}

	if err != nil {
		return nil, err
	}

	return util.GetPodFromDisplayName(h.kubeClient, displayName, podId)
}

// getProviderPod, "easy" means that we will get Pod info from Entity properties
// TODO: ask sdk-team to fix the bug, the entity properties are missing in the action.
func (h *HorizontalScaler) getProviderPod_easy(action *proto.ActionItemDTO) (*api.Pod, error) {
	targetType := action.GetTargetSE().GetEntityType()
	glog.V(3).Infof("Horizontal-Scale target type is %v", targetType.String())

	//1. get Entity Properties
	var properties []*proto.EntityDTO_EntityProperty
	switch targetType {
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
	getOption := metav1.GetOptions{}
	pod, err := h.kubeClient.CoreV1().Pods(nameSpace).Get(podName, getOption)
	if err != nil {
		glog.Errorf("Failed to get Pod(%s/%s): %v", nameSpace, podName, err)
		return nil, fmt.Errorf("Failed to get Pod from k8sAPI.")
	}
	return pod, nil
}
