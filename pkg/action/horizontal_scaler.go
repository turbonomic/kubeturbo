package action

import (
	"fmt"
	"strings"

	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"

	"github.com/vmturbo/kubeturbo/pkg/registry"
	"github.com/vmturbo/kubeturbo/pkg/storage"

	"github.com/vmturbo/vmturbo-go-sdk/sdk"

	"github.com/golang/glog"
)

type HorizontalScaler struct {
	kubeClient  *client.Client
	etcdStorage storage.Storage
}

// Create new VMT Actor. Must specify the kubernetes client.
func NewHorizontalScaler(client *client.Client, etcdStorage storage.Storage) *HorizontalScaler {
	return &HorizontalScaler{
		kubeClient:  client,
		etcdStorage: etcdStorage,
	}
}

func (this *HorizontalScaler) ScaleOut(actionItem *sdk.ActionItemDTO, msgID int32) error {
	if actionItem == nil {
		return fmt.Errorf("ActionItem passed in is nil")
	}
	targetEntityType := actionItem.GetTargetSE().GetEntityType()
	if targetEntityType == sdk.EntityDTO_CONTAINER_POD ||
		targetEntityType == sdk.EntityDTO_APPLICATION {

		providerPod, err := this.getProviderPod(actionItem)
		if err != nil {
			return fmt.Errorf("Cannot find provider pod: %s", err)
		}

		podNamespace := providerPod.Namespace
		podName := providerPod.Name
		podIdentifier := podNamespace + "/" + podName

		targetReplicationController, err := FindReplicationControllerForPod(this.kubeClient, providerPod)
		if err != nil {
			return fmt.Errorf("Error getting replication controller related to pod %s: %s", podIdentifier, err)
		}
		err = this.ProvisionPods(targetReplicationController)
		if err != nil {
			return fmt.Errorf("Error provision pod %s: %s", podIdentifier, err)
		}

		vmtEvents := registry.NewVMTEvents(this.kubeClient, "", this.etcdStorage)
		event := registry.GenerateVMTEvent(ActionProvision, targetReplicationController.Namespace, targetReplicationController.Name, "not specified", int(msgID))
		glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))
		_, errorPost := vmtEvents.Create(event)
		if errorPost != nil {
			return fmt.Errorf("Error creating VMTEvent for provisioning Pod %s: %s\n", podIdentifier, errorPost)
		}
		return nil
	}
	return fmt.Errorf("Entity type %v is not supported for horizontal scaling out", targetEntityType)
}

func (this *HorizontalScaler) getProviderPod(actionItem *sdk.ActionItemDTO) (*api.Pod, error) {
	targetEntityType := actionItem.GetTargetSE().GetEntityType()
	var providerPod *api.Pod
	if targetEntityType == sdk.EntityDTO_CONTAINER_POD {
		targetPod := actionItem.GetTargetSE()
		id := targetPod.GetId()
		foundPod, err := GetPodFromIdentifier(this.kubeClient, id)
		if err != nil {
			return nil, err
		}
		providerPod = foundPod
	} else if targetEntityType == sdk.EntityDTO_APPLICATION {
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

func (this *HorizontalScaler) ScaleIn(actionItem *sdk.ActionItemDTO, msgID int32) error {
	currentSE := actionItem.GetCurrentSE()
	targetEntityType := currentSE.GetEntityType()

	// TODO, currently UNBIND action is sent in as MOVE. Need to change in the future.
	if targetEntityType == sdk.EntityDTO_APPLICATION {
		// TODO find the pod name based on application ID. App id is in the following format.
		// !!
		// ProcessName::PodNamespace/PodName
		// NOT GOOD. Will change Later!
		appName := currentSE.GetId()
		ids := strings.Split(appName, "::")
		if len(ids) < 2 {
			return fmt.Errorf("%s is not a valid Application ID. Unbind failed.", appName)
		}

		podIdentifier := ids[1]

		podNamespace, _, err := ProcessPodIdentifier(podIdentifier)
		if err != nil {
			return err
		}

		providerPod, err := GetPodFromIdentifier(this.kubeClient, podIdentifier)
		if err != nil {
			return err
		}

		targetReplicationController, err := FindReplicationControllerForPod(this.kubeClient, providerPod)
		if err != nil {
			return err
		}
		err = this.UnbindPods(targetReplicationController)
		if err != nil {
			return err
		}

		vmtEvents := registry.NewVMTEvents(this.kubeClient, "", this.etcdStorage)
		event := registry.GenerateVMTEvent(ActionUnbind, podNamespace, targetReplicationController.Name, "not specified", int(msgID))
		glog.V(4).Infof("vmt event is %v, msgId is %d, %d", event, msgID, int(msgID))
		_, errorPost := vmtEvents.Create(event)
		if errorPost != nil {
			return fmt.Errorf("Error posting vmtevent: %s\n", errorPost)
		}
		return nil
	}
	return fmt.Errorf("Entity type %v is not supported for horizontal scaling in", targetEntityType)
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

func (this *HorizontalScaler) updateReplicationControllerReplicas(rc *api.ReplicationController, newReplicas int) error {
	rc.Spec.Replicas = newReplicas
	namespace := rc.Namespace
	newRC, err := this.kubeClient.ReplicationControllers(namespace).Update(rc)
	if err != nil {
		return fmt.Errorf("Error updating replication controller %s/%s: %s", rc.Namespace, rc.Name, err)
	}
	glog.V(4).Infof("New replicas of %s/%s is %d", newRC.Namespace, newRC.Name, newRC.Spec.Replicas)
	return nil
}
