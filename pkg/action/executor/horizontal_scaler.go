package executor

import (
	"fmt"
	"strings"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/apis/extensions"
	client "k8s.io/kubernetes/pkg/client/unversioned"

	"github.com/vmturbo/kubeturbo/pkg/action/turboaction"
	"github.com/vmturbo/kubeturbo/pkg/action/util"
	"github.com/vmturbo/kubeturbo/pkg/discovery/probe"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"errors"
)

type HorizontalScaler struct {
	kubeClient *client.Client
}

// Create new VMT Actor. Must specify the kubernetes client.
func NewHorizontalScaler(client *client.Client) *HorizontalScaler {
	return &HorizontalScaler{
		kubeClient: client,
	}
}

func (h *HorizontalScaler) ScaleOut(actionItem *proto.ActionItemDTO, msgID int32) (*turboaction.TurboAction, error) {
	if actionItem == nil {
		return nil, errors.New("ActionItem passed in is nil")
	}
	targetEntityType := actionItem.GetTargetSE().GetEntityType()
	if targetEntityType == proto.EntityDTO_CONTAINER_POD ||
		targetEntityType == proto.EntityDTO_APPLICATION {

		providerPod, err := h.getProviderPod(actionItem)
		if err != nil {
			return nil, fmt.Errorf("Cannot find provider pod: %s", err)
		}

		podNamespace := providerPod.Namespace
		podName := providerPod.Name
		podIdentifier := podNamespace + "/" + podName

		targetObject := &turboaction.TargetObject{
			podName,
			providerPod.Kind,
		}
		var parentObjRef *turboaction.ParentObjectRef
		parentRefObject, _ := probe.FindParentReferenceObject(providerPod)
		if parentRefObject != nil {
			parentObjRef = &turboaction.ParentObjectRef{
				parentRefObject.Name,
				parentRefObject.Kind,
			}
		} else {
			return nil, errors.New("Cannot find replication controller or deployment related to the pod")
		}

		if parentRefObject.Kind == turboaction.TypeReplicationController {
			rc, _ := util.FindReplicationControllerForPod(h.kubeClient, providerPod)
			scaleSpec := turboaction.ScaleSpec{
				rc.Spec.Replicas,
				rc.Spec.Replicas + 1,
			}
			content := turboaction.NewVMTEventContentBuilder(turboaction.ActionProvision, targetObject, int(msgID)).
				ActionSpec(scaleSpec).ParentObjectRef(parentObjRef).Build()
			event := turboaction.NewVMTEventBuilder(rc.Namespace).Content(content).Create()
			glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))

			err := h.ProvisionPods(rc)
			if err != nil {
				return nil, fmt.Errorf("Error provision pod %s: %s", podIdentifier, err)
			}

			return &event, nil
		} else if parentRefObject.Kind == turboaction.TypeReplicaSet {
			deployment, _ := util.FindDeploymentForPod(h.kubeClient, providerPod)
			scaleSpec := turboaction.ScaleSpec{
				deployment.Spec.Replicas,
				deployment.Spec.Replicas + 1,
			}
			content := turboaction.NewVMTEventContentBuilder(turboaction.ActionProvision, targetObject, int(msgID)).
				ActionSpec(scaleSpec).ParentObjectRef(parentObjRef).Build()
			event := turboaction.NewVMTEventBuilder(deployment.Namespace).Content(content).Create()
			glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))

			err = h.ProvisionPodsWithDeployments(deployment)
			if err != nil {
				return nil, fmt.Errorf("Error provision pod %s: %s", podIdentifier, err)
			}

			return &event, nil
		} else {
			return nil, fmt.Errorf("Error Scale Pod for %s-%s: Not Supported.", parentObjRef.ParentObjectType, parentObjRef.ParentObjectName)
		}

	}
	return nil, fmt.Errorf("Entity type %s is not supported for horizontal scaling out", targetEntityType)

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
	}
	return providerPod, nil
}

// Update replica of the target replication controller.
func (h *HorizontalScaler) ProvisionPods(targetReplicationController *api.ReplicationController) error {
	newReplicas := targetReplicationController.Spec.Replicas + 1

	err := h.updateReplicationControllerReplicas(targetReplicationController, newReplicas)
	if err != nil {
		return err
	}
	return nil
}

// Update replica of the target replication controller.
func (h *HorizontalScaler) ProvisionPodsWithDeployments(targetDeployment *extensions.Deployment) error {
	newReplicas := targetDeployment.Spec.Replicas + 1

	err := h.updateDeploymentReplicas(targetDeployment, newReplicas)
	if err != nil {
		return err
	}
	return nil
}

func (h *HorizontalScaler) ScaleIn(actionItem *proto.ActionItemDTO, msgID int32) (*turboaction.TurboAction, error) {
	currentSE := actionItem.GetCurrentSE()
	targetEntityType := currentSE.GetEntityType()

	// TODO, currently UNBIND action is sent in as MOVE. Need to change in the future.
	if targetEntityType == proto.EntityDTO_APPLICATION {
		// TODO find the pod name based on application ID. App id is in the following format.
		// !!
		// ProcessName::PodNamespace/PodName
		// NOT GOOD. Will change Later!
		appName := currentSE.GetId()
		ids := strings.Split(appName, "::")
		if len(ids) < 2 {
			return nil, fmt.Errorf("%s is not a valid Application ID. Unbind failed.", appName)
		}
		podIdentifier := ids[1]

		// Get the target pod from podIdentifier.
		providerPod, err := util.GetPodFromIdentifier(h.kubeClient, podIdentifier)
		if err != nil {
			return nil, err
		}

		targetObj := &turboaction.TargetObject{
			providerPod.Name,
			turboaction.TypePod,
		}
		var parentObjRef *turboaction.ParentObjectRef
		parentRefObject, _ := probe.FindParentReferenceObject(providerPod)
		if parentRefObject != nil {
			parentObjRef = &turboaction.ParentObjectRef{
				parentRefObject.Name,
				parentRefObject.Kind,
			}
		} else {
			//TODO, if this pod does not have a parentRefObject, there should be no way to scale. Need to be verified!
			return nil, fmt.Errorf("This pod %s is not able to be scaled.", podIdentifier)
		}

		if parentRefObject.Kind == turboaction.TypeReplicationController {
			// TODO. change this. Find rc based on its name and namespace.
			rc, _ := util.FindReplicationControllerForPod(h.kubeClient, providerPod)
			scaleSpec := &turboaction.ScaleSpec{
				rc.Spec.Replicas,
				rc.Spec.Replicas - 1,
			}

			content := turboaction.NewVMTEventContentBuilder(turboaction.ActionUnbind, targetObj, int(msgID)).
				ActionSpec(scaleSpec).ParentObjectRef(parentObjRef).Build()
			event := turboaction.NewVMTEventBuilder(rc.Namespace).Content(content).Create()
			glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))

			err = h.UnbindPods(rc)
			if err != nil {
				return nil, err
			}

			return &event, nil
		} else if parentRefObject.Kind == turboaction.TypeReplicaSet {
			// If parent object is a replica set, we need to find its deployment.
			deployment, _ := util.FindDeploymentForPod(h.kubeClient, providerPod)
			scaleSpec := &turboaction.ScaleSpec{
				deployment.Spec.Replicas,
				deployment.Spec.Replicas - 1,
			}

			content := turboaction.NewVMTEventContentBuilder(turboaction.ActionUnbind, targetObj, int(msgID)).
				ActionSpec(scaleSpec).ParentObjectRef(parentObjRef).Build()
			event := turboaction.NewVMTEventBuilder(deployment.Namespace).Content(content).Create()
			glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))

			err = h.UnbindPodsWithDeployment(deployment)
			if err != nil {
				return nil, err
			}

			return &event, nil
		} else {
			return nil, fmt.Errorf("Error Scale Pod for %s-%s: Not Supported.", parentObjRef.ParentObjectType, parentObjRef.ParentObjectName)
		}
	}
	return nil, fmt.Errorf("Entity type %s is not supported for horizontal scaling in", targetEntityType)
}

// Update replica of the target replication controller.
func (h *HorizontalScaler) UnbindPods(targetReplicationController *api.ReplicationController) error {
	newReplicas := targetReplicationController.Spec.Replicas - 1
	if newReplicas < 0 {
		return fmt.Errorf("Replica of %s/%s is already 0. Cannot scale in anymore.", targetReplicationController.Namespace, targetReplicationController.Name)
	}

	err := h.updateReplicationControllerReplicas(targetReplicationController, newReplicas)
	if err != nil {
		return err
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

// Update replica of the target replication controller.
func (h *HorizontalScaler) UnbindPodsWithDeployment(deployment *extensions.Deployment) error {
	newReplicas := deployment.Spec.Replicas - 1
	if newReplicas < 0 {
		return fmt.Errorf("Replica of %s/%s is already 0. Cannot scale in anymore.", deployment.Namespace, deployment.Name)
	}

	err := h.updateDeploymentReplicas(deployment, newReplicas)
	if err != nil {
		return err
	}
	return nil
}

func (h *HorizontalScaler) updateDeploymentReplicas(deployment *extensions.Deployment, newReplicas int32) error {
	deployment.Spec.Replicas = newReplicas
	namespace := deployment.Namespace
	newDeployment, err := h.kubeClient.Deployments(namespace).Update(deployment)
	if err != nil {
		return fmt.Errorf("Error updating replication controller %s/%s: %s", deployment.Namespace, deployment.Name, err)
	}
	glog.V(4).Infof("New replicas of %s/%s is %d", newDeployment.Namespace, newDeployment.Name, newDeployment.Spec.Replicas)
	return nil
}
