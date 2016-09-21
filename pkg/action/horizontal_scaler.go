package action

import (
	"fmt"
	"strings"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/apis/extensions"
	client "k8s.io/kubernetes/pkg/client/unversioned"

	"github.com/vmturbo/kubeturbo/pkg/registry"

	"github.com/vmturbo/vmturbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

type HorizontalScaler struct {
	kubeClient       *client.Client
	vmtEventRegistry *registry.VMTEventRegistry
}

// Create new VMT Actor. Must specify the kubernetes client.
func NewHorizontalScaler(client *client.Client, vmtEventRegistry *registry.VMTEventRegistry) *HorizontalScaler {
	return &HorizontalScaler{
		kubeClient:       client,
		vmtEventRegistry: vmtEventRegistry,
	}
}

func (this *HorizontalScaler) ScaleOut(actionItem *proto.ActionItemDTO, msgID int32) (*registry.VMTEvent, error) {
	if actionItem == nil {
		return nil, fmt.Errorf("ActionItem passed in is nil")
	}
	targetEntityType := actionItem.GetTargetSE().GetEntityType()
	fmt.Printf("is containerPod: %v; want %s, got %s\n", targetEntityType == proto.EntityDTO_CONTAINER_POD, targetEntityType, proto.EntityDTO_CONTAINER_POD)
	if targetEntityType == proto.EntityDTO_CONTAINER_POD ||
		targetEntityType == proto.EntityDTO_APPLICATION {

		providerPod, err := this.getProviderPod(actionItem)
		if err != nil {
			return nil, fmt.Errorf("Cannot find provider pod: %s", err)
		}

		podNamespace := providerPod.Namespace
		podName := providerPod.Name
		podIdentifier := podNamespace + "/" + podName

		rc, _ := FindReplicationControllerForPod(this.kubeClient, providerPod)
		deployment, _ := FindDeploymentForPod(this.kubeClient, providerPod)
		if rc == nil && deployment == nil {
			return nil, fmt.Errorf("Cannot find replication controller or deployment related to the pod")
		}

		if rc != nil {
			content := registry.NewVMTEventContentBuilder(ActionProvision, rc.Name, int(msgID)).
				ScaleSpec(rc.Spec.Replicas, rc.Spec.Replicas+1).Build()
			event := registry.NewVMTEventBuilder(rc.Namespace).Content(content).Create()
			glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))

			_, errorPost := this.vmtEventRegistry.Create(&event)
			if errorPost != nil {
				return nil, fmt.Errorf("Error posting VMTEvent %s for %s", content.ActionType, content.TargetSE)
			}

			err = this.ProvisionPods(rc)
			if err != nil {
				return nil, fmt.Errorf("Error provision pod %s: %s", podIdentifier, err)
			}

			return &event, nil
		}

		if deployment != nil {
			content := registry.NewVMTEventContentBuilder(ActionProvision, deployment.Name, int(msgID)).
				ScaleSpec(deployment.Spec.Replicas, deployment.Spec.Replicas+1).Build()
			event := registry.NewVMTEventBuilder(deployment.Namespace).Content(content).Create()
			glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))

			_, errorPost := this.vmtEventRegistry.Create(&event)
			if errorPost != nil {
				return nil, fmt.Errorf("Error posting VMTEvent %s for %s", content.ActionType, content.TargetSE)
			}

			err = this.ProvisionPodsWithDeployments(deployment)
			if err != nil {
				return nil, fmt.Errorf("Error provision pod %s: %s", podIdentifier, err)
			}

			return &event, nil
		}
	}
	return nil, fmt.Errorf("Entity type %v is not supported for horizontal scaling out", targetEntityType)

}

func (this *HorizontalScaler) getProviderPod(actionItem *proto.ActionItemDTO) (*api.Pod, error) {
	targetEntityType := actionItem.GetTargetSE().GetEntityType()
	var providerPod *api.Pod
	if targetEntityType == proto.EntityDTO_CONTAINER_POD {
		targetPod := actionItem.GetTargetSE()
		id := targetPod.GetId()
		foundPod, err := GetPodFromIdentifier(this.kubeClient, id)
		if err != nil {
			return nil, err
		}
		providerPod = foundPod
	} else if targetEntityType == proto.EntityDTO_APPLICATION {
		providers := actionItem.GetProviders()

		foundPod, err := FindApplicationPodProvider(this.kubeClient, providers)
		if err != nil {
			return nil, err
		}
		providerPod = foundPod
	}
	return providerPod, nil
}

// Update replica of the target replication controller.
func (this *HorizontalScaler) ProvisionPods(targetReplicationController *api.ReplicationController) error {
	newReplicas := targetReplicationController.Spec.Replicas + 1

	err := this.updateReplicationControllerReplicas(targetReplicationController, newReplicas)
	if err != nil {
		return err
	}
	return nil
}

// Update replica of the target replication controller.
func (this *HorizontalScaler) ProvisionPodsWithDeployments(targetDeployment *extensions.Deployment) error {
	newReplicas := targetDeployment.Spec.Replicas + 1

	err := this.updateDeploymentReplicas(targetDeployment, newReplicas)
	if err != nil {
		return err
	}
	return nil
}

func (this *HorizontalScaler) ScaleIn(actionItem *proto.ActionItemDTO, msgID int32) (*registry.VMTEvent, error) {
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

		providerPod, err := GetPodFromIdentifier(this.kubeClient, podIdentifier)
		if err != nil {
			return nil, err
		}

		rc, _ := FindReplicationControllerForPod(this.kubeClient, providerPod)

		deployment, _ := FindDeploymentForPod(this.kubeClient, providerPod)
		if rc == nil && deployment == nil {
			return nil, fmt.Errorf("Error getting replication controller and deployment for pod")
		}

		if rc != nil {
			content := registry.NewVMTEventContentBuilder(ActionUnbind, rc.Name, int(msgID)).
				ScaleSpec(rc.Spec.Replicas, rc.Spec.Replicas-1).Build()
			event := registry.NewVMTEventBuilder(rc.Namespace).Content(content).Create()
			glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))

			_, errorPost := this.vmtEventRegistry.Create(&event)
			if errorPost != nil {
				return nil, fmt.Errorf("Error posting VMTEvent %s for %s", content.ActionType, content.TargetSE)
			}

			err = this.UnbindPods(rc)
			if err != nil {
				return nil, err
			}

			return &event, nil
		}
		if deployment != nil {
			content := registry.NewVMTEventContentBuilder(ActionUnbind, deployment.Name, int(msgID)).
				ScaleSpec(deployment.Spec.Replicas, deployment.Spec.Replicas-1).Build()
			event := registry.NewVMTEventBuilder(deployment.Namespace).Content(content).Create()
			glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))

			_, errorPost := this.vmtEventRegistry.Create(&event)
			if errorPost != nil {
				return nil, fmt.Errorf("Error posting VMTEvent %s for %s", content.ActionType, content.TargetSE)
			}

			err = this.UnbindPodsWithDeployment(deployment)
			if err != nil {
				return nil, err
			}

			return &event, nil
		}
	}
	return nil, fmt.Errorf("Entity type %v is not supported for horizontal scaling in", targetEntityType)
}

// Update replica of the target replication controller.
func (this *HorizontalScaler) UnbindPods(targetReplicationController *api.ReplicationController) error {
	newReplicas := targetReplicationController.Spec.Replicas - 1
	if newReplicas < 0 {
		return fmt.Errorf("Replica of %s/%s is already 0. Cannot scale in anymore.", targetReplicationController.Namespace, targetReplicationController.Name)
	}

	err := this.updateReplicationControllerReplicas(targetReplicationController, newReplicas)
	if err != nil {
		return err
	}
	return nil
}

func (this *HorizontalScaler) updateReplicationControllerReplicas(rc *api.ReplicationController, newReplicas int32) error {
	rc.Spec.Replicas = newReplicas
	namespace := rc.Namespace
	newRC, err := this.kubeClient.ReplicationControllers(namespace).Update(rc)
	if err != nil {
		return fmt.Errorf("Error updating replication controller %s/%s: %s", rc.Namespace, rc.Name, err)
	}
	glog.V(4).Infof("New replicas of %s/%s is %d", newRC.Namespace, newRC.Name, newRC.Spec.Replicas)
	return nil
}

// Update replica of the target replication controller.
func (this *HorizontalScaler) UnbindPodsWithDeployment(deployment *extensions.Deployment) error {
	newReplicas := deployment.Spec.Replicas - 1
	if newReplicas < 0 {
		return fmt.Errorf("Replica of %s/%s is already 0. Cannot scale in anymore.", deployment.Namespace, deployment.Name)
	}

	err := this.updateDeploymentReplicas(deployment, newReplicas)
	if err != nil {
		return err
	}
	return nil
}

func (this *HorizontalScaler) updateDeploymentReplicas(deployment *extensions.Deployment, newReplicas int32) error {
	deployment.Spec.Replicas = newReplicas
	namespace := deployment.Namespace
	newDeployment, err := this.kubeClient.Deployments(namespace).Update(deployment)
	if err != nil {
		return fmt.Errorf("Error updating replication controller %s/%s: %s", deployment.Namespace, deployment.Name, err)
	}
	glog.V(4).Infof("New replicas of %s/%s is %d", newDeployment.Namespace, newDeployment.Name, newDeployment.Spec.Replicas)
	return nil
}
