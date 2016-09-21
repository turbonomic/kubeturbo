package supervisor

import (
	"fmt"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/cache"
	"k8s.io/kubernetes/pkg/util/wait"

	client "k8s.io/kubernetes/pkg/client/unversioned"

	vmtcache "github.com/vmturbo/kubeturbo/pkg/cache"
	"github.com/vmturbo/kubeturbo/pkg/registry"
	"github.com/vmturbo/kubeturbo/pkg/storage"

	"github.com/golang/glog"
)

type EventCheckFunc func(event *registry.VMTEvent) (bool, error)

type ActionSupervisorConfig struct {
	etcdStorage    storage.Storage
	kubeClient     *client.Client
	VMTEventQueue  *vmtcache.HashedFIFO
	StopEverything chan struct{}
}

func NewActionSupervisorConfig(kubeClient *client.Client, etcdStorage storage.Storage) *ActionSupervisorConfig {
	config := &ActionSupervisorConfig{
		etcdStorage: etcdStorage,
		kubeClient:  kubeClient,

		VMTEventQueue:  vmtcache.NewHashedFIFO(cache.MetaNamespaceKeyFunc),
		StopEverything: make(chan struct{}),
	}

	vmtcache.NewReflector(config.createExecutedVMTEventsLW(), &registry.VMTEvent{},
		config.VMTEventQueue, 0).RunUntil(config.StopEverything)

	return config

}

func (config *ActionSupervisorConfig) createExecutedVMTEventsLW() *vmtcache.ListWatch {
	return vmtcache.NewListWatchFromStorage(config.etcdStorage, "vmtevents", api.NamespaceAll,
		func(obj interface{}) bool {
			vmtEvent, ok := obj.(*registry.VMTEvent)
			if !ok {
				return false
			}
			if vmtEvent.Status == registry.Executed {
				return true
			}
			return false
		})
}

// ActionSupervisor verifies if an exectued action succeed or failed.
type VMTActionSupervisor struct {
	config           *ActionSupervisorConfig
	vmtEventRegistry *registry.VMTEventRegistry
}

func NewActionSupervisor(config *ActionSupervisorConfig) *VMTActionSupervisor {
	vmtEventRegistry := registry.NewVMTEventRegistry(config.etcdStorage)

	return &VMTActionSupervisor{
		config:           config,
		vmtEventRegistry: vmtEventRegistry,
	}
}

func (this *VMTActionSupervisor) Start() {
	go wait.Until(this.getNextVMTEvent, 0, this.config.StopEverything)

}

func (this *VMTActionSupervisor) getNextVMTEvent() {
	e, err := this.config.VMTEventQueue.Pop(nil)
	if err != nil {
		// TODO
	}
	event := e.(*registry.VMTEvent)

	glog.V(3).Infof("Executed event is %v", event)
	// TODO use agent to verify if the event secceeds
	switch {
	case event.Content.ActionType == "move":
		this.updateVMTEvent(event, this.checkMoveAction)
	case event.Content.ActionType == "provision":
		this.updateVMTEvent(event, this.checkProvisionAction)
	case event.Content.ActionType == "unbind":
		this.updateVMTEvent(event, this.checkUnbindAction)
	}
}

func (this *VMTActionSupervisor) checkMoveAction(event *registry.VMTEvent) (bool, error) {
	if event.Content.ActionType != "move" {
		return false, fmt.Errorf("Not a move action")
	}
	podName := event.Content.TargetSE
	podNamespace := event.Namespace
	podIdentifier := podNamespace + "/" + podName

	targetPod, err := this.config.kubeClient.Pods(podNamespace).Get(podName)
	if err != nil {
		return false, fmt.Errorf("Cannot find pod %s in cluster", podIdentifier)
	}
	moveDestination := event.Content.MoveSpec.Destination
	actualHostingNode := targetPod.Spec.NodeName
	if actualHostingNode == moveDestination {
		return true, nil
	}
	return false, nil
}

func (this *VMTActionSupervisor) checkProvisionAction(event *registry.VMTEvent) (bool, error) {
	if event.Content.ActionType != "provision" {
		return false, fmt.Errorf("Not a provision action")
	}
	return this.checkScaleAction(event)
}

func (this *VMTActionSupervisor) checkUnbindAction(event *registry.VMTEvent) (bool, error) {
	if event.Content.ActionType != "unbind" {
		return false, fmt.Errorf("Not a unbind action")
	}
	return this.checkScaleAction(event)
}

func (this *VMTActionSupervisor) checkScaleAction(event *registry.VMTEvent) (bool, error) {
	name := event.Content.TargetSE
	namespace := event.Namespace
	identifier := namespace + "/" + name

	targetRC, _ := this.config.kubeClient.ReplicationControllers(namespace).Get(name)
	targetDeployment, _ := this.config.kubeClient.Deployments(namespace).Get(name)

	if (targetDeployment == nil && targetRC == nil) ||
		(targetDeployment.Name == "" && targetRC.Name == "") {
		return false, fmt.Errorf("Cannot find replication controller or deployment %s in cluster", identifier)
	}

	targetReplicas := event.Content.ScaleSpec.NewReplicas
	currentReplicas := 0
	if targetRC != nil && targetRC.Name != "" {
		currentReplicas = targetRC.Spec.Replicas
		glog.V(4).Infof("currentReplicas from RC is %d", currentReplicas)
	} else if targetDeployment != nil && targetDeployment.Name != "" {
		currentReplicas = targetDeployment.Spec.Replicas
		glog.V(4).Infof("currentReplicas from Deployment is %d", currentReplicas)
	}
	glog.V(4).Infof("replica want %d, is %d", targetReplicas, currentReplicas)
	if targetReplicas == currentReplicas {
		return true, nil
	}
	return false, nil
}

func (this *VMTActionSupervisor) updateVMTEvent(event *registry.VMTEvent, checkFunc EventCheckFunc) {
	successful, err := checkFunc(event)
	if err != nil {
		glog.Errorf("Error checking move action: %s", err)
		return
	}
	if successful {
		if err = this.vmtEventRegistry.UpdateStatus(event, registry.Success); err != nil {
			glog.Errorf("Failed to update event %s status to %s: %s", event.Name, registry.Success, err)
		}
		return
	}
	// Check if the event has expired. If true, update the status to fail and return;
	// Otherwise, only update the LastTimestamp.
	expired := checkExpired(event)
	glog.Infof("checkExpired result %v", expired)
	if expired {
		glog.Errorf("Timeout processing %s event on %s",
			event.Content.ActionType, event.Content.TargetSE)
		if err = this.vmtEventRegistry.UpdateStatus(event, registry.Fail); err != nil {
			glog.Errorf("Failed to update event %s status to %s: %s", event.Name, registry.Fail, err)
		}
		return
	}
	time.Sleep(time.Second * 1)
	if err = this.vmtEventRegistry.UpdateTimestamp(event); err != nil {
		glog.Errorf("Failed to update timestamp of event %s", event.Name, err)
	}
	return
}

func checkExpired(event *registry.VMTEvent) bool {
	now := time.Now()
	duration := now.Sub(event.LastTimestamp)
	glog.Infof("Duration is %v", duration)
	if duration > time.Duration(10)*time.Second {
		return true
	}
	return false
}
