package executor

import (
	"errors"
	"fmt"
	"strings"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/apis/extensions"
	client "k8s.io/kubernetes/pkg/client/unversioned"

	"github.com/vmturbo/kubeturbo/pkg/action/turboaction"
	"github.com/vmturbo/kubeturbo/pkg/action/util"
	"github.com/vmturbo/kubeturbo/pkg/discovery/probe"
	turboscheduler "github.com/vmturbo/kubeturbo/pkg/scheduler"
	"github.com/vmturbo/kubeturbo/pkg/turbostore"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

type HorizontalScaler struct {
	kubeClient *client.Client
	broker     turbostore.Broker
	scheduler  *turboscheduler.TurboScheduler
}

func NewHorizontalScaler(client *client.Client, broker turbostore.Broker, scheduler *turboscheduler.TurboScheduler) *HorizontalScaler {
	return &HorizontalScaler{
		kubeClient: client,
		broker:     broker,
		scheduler:  scheduler,
	}
}

func (h *HorizontalScaler) Execute(actionItem *proto.ActionItemDTO) (*turboaction.TurboAction, error) {
	if actionItem == nil {
		return nil, errors.New("ActionItem passed in is nil")
	}
	action, err := h.buildPendingScalingTurboAction(actionItem)
	if err != nil {
		return nil, err
	}

	return h.horizontalScale(action)
}

func (h *HorizontalScaler) buildPendingScalingTurboAction(actionItem *proto.ActionItemDTO) (*turboaction.TurboAction,
	error) {
	targetSE := actionItem.GetTargetSE()
	targetEntityType := targetSE.GetEntityType()
	if targetEntityType != proto.EntityDTO_CONTAINER_POD || targetEntityType != proto.EntityDTO_APPLICATION {
		return nil, errors.New("The target service entity for scaling action is " +
			"neither a Pod nor an Application.")
	}

	providerPod, err := h.getProviderPod(actionItem)
	if err != nil {
		return nil, fmt.Errorf("Try to scaling %s, but cannot find a pod related to it in the cluster: %s",
			targetSE.GetId(), err)
	}

	targetObject := &turboaction.TargetObject{
		TargetObjectUID:       string(providerPod.UID),
		TargetObjectNamespace: providerPod.Namespace,
		TargetObjectName:      providerPod.Name,
		TargetObjectType:      turboaction.TypePod,
	}

	var parentObjRef *turboaction.ParentObjectRef
	parentRefObject, _ := probe.FindParentReferenceObject(providerPod)
	if parentRefObject != nil {
		parentObjRef = &turboaction.ParentObjectRef{
			ParentObjectUID:       string(parentRefObject.UID),
			ParentObjectNamespace: parentRefObject.Namespace,
			ParentObjectName:      parentRefObject.Name,
			ParentObjectType:      parentRefObject.Kind,
		}
	} else {
		return nil, errors.New("Cannot automatically scale")
	}

	// Get diff and action type according scale in or scale out.
	var diff int32
	var actionType turboaction.TurboActionType
	if actionItem.GetActionType() == proto.ActionItemDTO_PROVISION {
		// Scale out, increase the replica. diff = 1.
		diff = 1
		actionType = turboaction.ActionProvision
	} else if actionItem.GetActionType() == proto.ActionItemDTO_MOVE {
		// Scale in, decrease the replica. diff = -1.
		diff = -1
		actionType = turboaction.ActionUnbind
	} else {
		return nil, errors.New("Not a scaling action.")
	}

	var scaleSpec turboaction.ScaleSpec
	switch parentRefObject.Kind {
	case turboaction.TypeReplicationController:
		rc, err := util.FindReplicationControllerForPod(h.kubeClient, providerPod)
		if err != nil {
			return nil, fmt.Errorf("Failed to find replication controller for finishing the scaling "+
				"action: %s", err)
		}
		scaleSpec = turboaction.ScaleSpec{
			OriginalReplicas: rc.Spec.Replicas,
			NewReplicas:      rc.Spec.Replicas + diff,
		}
		break

	case turboaction.TypeReplicaSet:
		deployment, err := util.FindDeploymentForPod(h.kubeClient, providerPod)
		return nil, fmt.Errorf("Failed to find deployment for finishing the scaling "+
			"action: %s", err)
		scaleSpec = turboaction.ScaleSpec{
			OriginalReplicas: deployment.Spec.Replicas,
			NewReplicas:      deployment.Spec.Replicas + diff,
		}
		break

	default:
		return nil, fmt.Errorf("Error Scale Pod for %s-%s: Not Supported.",
			parentObjRef.ParentObjectType, parentObjRef.ParentObjectName)
	}

	// Invalid new replica.
	if scaleSpec.NewReplicas < 0 {
		return nil, fmt.Errorf("Cannot scale %s/%s from %d to %d", parentRefObject.Namespace,
			parentRefObject.Name, scaleSpec.OriginalReplicas, scaleSpec.NewReplicas)
	}

	content := turboaction.NewTurboActionContentBuilder(actionType, targetObject).
		ActionSpec(scaleSpec).
		ParentObjectRef(parentObjRef).
		Build()
	action := turboaction.NewTurboActionBuilder(parentObjRef.ParentObjectNamespace, *actionItem.Uuid).
		Content(content).
		Create()
	glog.V(4).Infof("Horizontal scaling action is built as %v", action)

	return &action, nil
}

func (h *HorizontalScaler) getProviderPod(actionItem *proto.ActionItemDTO) (*api.Pod, error) {
	targetEntityType := actionItem.GetTargetSE().GetEntityType()
	var providerPod *api.Pod
	if targetEntityType == proto.EntityDTO_CONTAINER_POD {
		targetPod := actionItem.GetTargetSE()
		id := targetPod.GetId()
		foundPod, err := util.GetPodFromIdentifier(h.kubeClient, id)
		if err != nil {
			return nil, err
		}
		providerPod = foundPod
	} else if targetEntityType == proto.EntityDTO_APPLICATION {
		providers := actionItem.GetProviders()

		foundPod, err := util.FindApplicationPodProvider(h.kubeClient, providers)
		if err != nil {
			return nil, err
		}
		providerPod = foundPod
	} else if targetEntityType == proto.EntityDTO_VIRTUAL_APPLICATION {
		// TODO, unbind action is send as MOVE
		// An example actionItemDTO is as follows:

		// actionItemDTO:<actionType:MOVE uuid:"_YaxeQDPxEea2efRvkwVM-Q"
		// targetSE:<entityType:VIRTUAL_APPLICATION id:"vApp-apache2" displayName:"vApp-apache2" > currentSE:<
		// entityType:APPLICATION id:"apache2::default/frontend-hhp6g" displayName:"apache2::default/frontend-
		// hhp6g" > providers:<entityType:APPLICATION ids:"apache2::default/frontend-57ytg" ids:"apache2::
		// default/frontend-ue0kg" ids:"apache2::default/frontend-hhp6g" ids:"apache2::default/frontend-luohe">>
		//
		// TODO find the pod name based on application ID. App id is in the following format.
		// TODO, maybe use service related info to find the corresponding pod.
		// ProcessName::PodNamespace/PodName
		currentSE := actionItem.GetCurrentSE()
		targetEntityType := currentSE.GetEntityType()
		if targetEntityType != proto.EntityDTO_APPLICATION {

		}
		appName := currentSE.GetId()
		ids := strings.Split(appName, "::")
		if len(ids) < 2 {
			return nil, fmt.Errorf("%s is not a valid Application ID. Unbind failed.", appName)
		}
		podIdentifier := ids[1]

		// Get the target pod from podIdentifier.
		pod, err := util.GetPodFromIdentifier(h.kubeClient, podIdentifier)
		if err != nil {
			return nil, err
		}
		providerPod = pod
	}
	return providerPod, nil
}

func (h *HorizontalScaler) horizontalScale(action *turboaction.TurboAction) (*turboaction.TurboAction, error) {
	// 1. Setup consumer
	actionContent := action.Content
	scaleSpec, ok := actionContent.ActionSpec.(turboaction.ScaleSpec)
	if !ok || scaleSpec.NewReplicas < 0 {
		return nil, errors.New("Failed to setup horizontal scaler as the provided scale spec is invalid.")
	}

	var key string
	if actionContent.ParentObjectRef.ParentObjectUID != "" {
		key = actionContent.ParentObjectRef.ParentObjectUID
	} else {
		return nil, errors.New("Failed to setup horizontal scaler consumer: failed to retrieve the key.")
	}
	glog.V(3).Infof("The current horizontal scaler consumer is listening on the key %s", key)
	podConsumer := turbostore.NewPodConsumer(string(action.UID), key, h.broker)

	// 2. scale up and down by changing the replica of replication controller or deployment.
	err := h.updateReplica(actionContent.TargetObject, actionContent.ParentObjectRef, actionContent.ActionSpec)
	if err != nil {
		return nil, fmt.Errorf("Failed to update replica: %s", err)
	}

	// 3. If this is an unbind action, it means it is an action with only one stage.
	// So after changing the replica it can return immediately.
	if action.Content.ActionType == turboaction.ActionUnbind {
		// Update turbo action.
		action.Status = turboaction.Executed
		return action, nil
	}

	// 3. Wait for desired pending pod
	// TODO, should we always block here?
	pod, ok := <-podConsumer.WaitPod()
	if !ok {
		return nil, errors.New("Failed to receive the pending pod generated as a result of rescheduling.")
	}
	podConsumer.Leave(key, h.broker)

	// 4. Schedule the pod.
	// TODO: we don't have a destination to provision a pod yet. So here we need to call scheduler. Or we can post back the pod to be scheduled
	err = h.scheduler.Schedule(pod)
	if err != nil {
		return nil, fmt.Errorf("Error scheduling the new provisioned pod: %s", err)
	}

	// Update turbo action.
	action.Status = turboaction.Executed

	return action, nil
}

func (h *HorizontalScaler) updateReplica(targetObject turboaction.TargetObject, parentObjRef turboaction.ParentObjectRef,
	actionSpec turboaction.ActionSpec) error {
	scaleSpec, ok := actionSpec.(turboaction.ScaleSpec)
	if !ok {
		return fmt.Errorf("%++v is not a scale spec", actionSpec)
	}
	providerType := parentObjRef.ParentObjectType
	switch providerType {
	case turboaction.TypeReplicationController:
		rc, err := h.kubeClient.ReplicationControllers(parentObjRef.ParentObjectNamespace).
			Get(parentObjRef.ParentObjectName)
		if err != nil {
			return fmt.Errorf("Failed to find replication controller for finishing the scaling action: %s", err)
		}
		return h.updateReplicationControllerReplicas(rc, scaleSpec.NewReplicas)

	case turboaction.TypeReplicaSet:
		providerPod, err := h.kubeClient.Pods(targetObject.TargetObjectNamespace).Get(targetObject.TargetObjectName)
		if err != nil {
			return fmt.Errorf("Failed to find deployemnet for finishing the scaling action: %s", err)
		}
		// TODO, here we only support ReplicaSet created by Deployment.
		deployment, err := util.FindDeploymentForPod(h.kubeClient, providerPod)
		if err != nil {
			return fmt.Errorf("Failed to find deployment for finishing the scaling action: %s", err)
		}
		return h.updateDeploymentReplicas(deployment, scaleSpec.NewReplicas)
		break
	default:
		return fmt.Errorf("Unsupported provider type %s", providerType)
	}
	return nil
}

func (h *HorizontalScaler) updateReplicationControllerReplicas(rc *api.ReplicationController, newReplicas int32) error {
	rc.Spec.Replicas = newReplicas
	namespace := rc.Namespace
	newRC, err := h.kubeClient.ReplicationControllers(namespace).Update(rc)
	if err != nil {
		return fmt.Errorf("Error updating replication controller %s/%s: %s", rc.Namespace, rc.Name, err)
	}
	glog.V(4).Infof("New replicas of %s/%s is %d", newRC.Namespace, newRC.Name, newRC.Spec.Replicas)
	return nil
}

func (h *HorizontalScaler) updateDeploymentReplicas(deployment *extensions.Deployment, newReplicas int32) error {
	deployment.Spec.Replicas = newReplicas
	namespace := deployment.Namespace
	newDeployment, err := h.kubeClient.Deployments(namespace).Update(deployment)
	if err != nil {
		return fmt.Errorf("Error updating replication controller %s/%s: %s",
			deployment.Namespace, deployment.Name, err)
	}
	glog.V(4).Infof("New replicas of %s/%s is %d", newDeployment.Namespace, newDeployment.Name,
		newDeployment.Spec.Replicas)
	return nil
}
