package executor

import (
	"context"
	"fmt"
	"strings"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"

	apicorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/dynamic"
	typedClient "k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/action/executor/gitops"
	actionutil "github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	discoveryutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/features"
	"github.com/turbonomic/kubeturbo/pkg/util"
)

// k8sController defines a common interface for kubernetes controller actions
// Currently supported controllers include:
// - ReplicationController
// - ReplicaSet
// - Deployment
type k8sController interface {
	get(name string) (*k8sControllerSpec, error)
	update(updatedSpec *k8sControllerSpec) error
}

// k8sControllerSpec defines a set of objects that we want to update:
// - replicas: The replicas of a controller to update for horizontal scale
// - podSpec: The pod template of a controller to update for consistent resize
// Note: Use pointer for in-place update
type k8sControllerSpec struct {
	replicas       *int32
	podSpec        *apicorev1.PodSpec
	controllerName string
}

type kubeClients struct {
	typedClient *typedClient.Clientset
	dynClient   dynamic.Interface
	// TODO: remove the need of this as we have dynClient already
	dynNamespacedClient dynamic.ResourceInterface
}

type parentController struct {
	clients      kubeClients
	obj          *unstructured.Unstructured
	name         string
	ormClient    *resourcemapping.ORMClient
	managerApp   *repository.K8sApp
	gitConfig    gitops.GitConfig
	k8sClusterId string
}

func (c *parentController) get(name string) (*k8sControllerSpec, error) {
	obj, err := c.clients.dynNamespacedClient.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	objName := fmt.Sprintf("%s/%s", obj.GetNamespace(), name)
	kind := obj.GetKind()

	replicas := int64(0)
	found := false
	if kind != util.KindDaemonSet { // daemonsets do not have replica field
		replicas, found, err = unstructured.NestedInt64(obj.Object, "spec", "replicas")
		if err != nil || !found {
			return nil, fmt.Errorf("error retrieving replicas from %s %s: %v", kind, objName, err)
		}
	}

	podSpecUnstructured, found, err := unstructured.NestedFieldCopy(obj.Object, "spec", "template", "spec")
	if err != nil || !found {
		return nil, fmt.Errorf("error retrieving podSpec from %s %s: %v", kind, objName, err)
	}

	podSpec := apicorev1.PodSpec{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(podSpecUnstructured.(map[string]interface{}), &podSpec); err != nil {
		return nil, fmt.Errorf("error converting unstructured pod spec to typed pod spec for %s %s: %v", kind, objName, err)
	}

	c.obj = obj
	int32Replicas := int32(replicas)
	return &k8sControllerSpec{
		replicas:       &int32Replicas,
		podSpec:        &podSpec,
		controllerName: fmt.Sprintf("%s-%s", kind, objName),
	}, nil
}

func (c *parentController) update(updatedSpec *k8sControllerSpec) error {
	objName := fmt.Sprintf("%s/%s", c.obj.GetNamespace(), c.obj.GetName())
	kind := c.obj.GetKind()

	replicaVal := int64(*updatedSpec.replicas)
	podSpecUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(updatedSpec.podSpec)
	if err != nil {
		return fmt.Errorf("error converting pod spec to unstructured pod spec for %s %s: %v", kind, objName, err)
	}

	origControllerObj := c.obj.DeepCopy()
	if kind != util.KindDaemonSet { // daemonsets do not have replica field
		if err := unstructured.SetNestedField(c.obj.Object, replicaVal, "spec", "replicas"); err != nil {
			return fmt.Errorf("error setting replicas into unstructured %s %s: %v", kind, objName, err)
		}
	}
	if err := unstructured.SetNestedField(c.obj.Object, podSpecUnstructured, "spec", "template", "spec"); err != nil {
		return fmt.Errorf("error setting podSpec into unstructured %s %s: %v", kind, objName, err)
	}

	if c.managerApp != nil &&
		c.managerApp.Type != repository.AppTypeK8s &&
		utilfeature.DefaultFeatureGate.Enabled(features.GitopsApps) {
		var manager gitops.GitopsManager
		switch c.managerApp.Type {
		case repository.AppTypeArgoCD:
			// The workload is managed by a pipeline controller (argoCD) which replicates
			// it from a source of truth
			manager = gitops.NewGitManager(c.gitConfig, c.clients.typedClient,
				c.clients.dynClient, c.obj, c.managerApp, c.k8sClusterId)
			glog.Infof("Gitops pipeline detected.")
		default:
			return fmt.Errorf("unsupported gitops manager type: %v", c.managerApp.Type)
		}

		completionFn, completionData, err := manager.Update(int64(*updatedSpec.replicas), podSpecUnstructured)
		if err != nil {
			return fmt.Errorf("failed to update the gitops managed source of truth: %v", err)
		}
		return manager.WaitForActionCompletion(completionFn, completionData)
	}

	ownerInfo, isOwnerSet := discoveryutil.GetOwnerInfo(c.obj.GetOwnerReferences())
	if !c.shouldSkipOperator(c.obj) && isOwnerSet {
		// If k8s controller is controlled by custom controller, update the CR using OperatorResourceMapping
		// if SkipOperatorLabel is not set or not true.
		if c.ormClient == nil {
			return fmt.Errorf("failed to execute action with nil ORMClient")
		}
		err = c.ormClient.Update(origControllerObj, c.obj, ownerInfo)
	} else {
		_, err = c.clients.dynNamespacedClient.Update(context.TODO(), c.obj, metav1.UpdateOptions{})
		if utilfeature.DefaultFeatureGate.Enabled(features.AllowIncreaseNsQuota4Resizing) {
			err4Waiting := c.waitForAllNewReplicasToBeCreated(c.obj.GetName())
			if err4Waiting != nil {
				glog.V(2).Infof("Get error while waiting for the new replicas to be created for the workload controller %s/%s:%v", c.obj.GetNamespace(), c.obj.GetName(), err4Waiting)
				if hasWarningEvent, warningInfo := c.getLatestWarningEvents(c.obj.GetNamespace(), c.obj.GetName()); hasWarningEvent {
					return fmt.Errorf(warningInfo)
				}
				return err4Waiting
			}
			glog.V(2).Infof("All of the new replicasets get created for the workload controller %s/%s", c.obj.GetNamespace(), c.obj.GetName())
		}
	}
	return err
}

// Wait for all of the new replicas to be created
func (c *parentController) waitForAllNewReplicasToBeCreated(name string) error {
	return wait.Poll(DefaultRetrySleepInterval, DefaultWaitReplicaToBeScheduled, func() (bool, error) {
		obj, errInternal := c.clients.dynNamespacedClient.Get(context.TODO(), name, metav1.GetOptions{})
		if errInternal != nil {
			return false, errInternal
		}

		var replicas, updatedReplicas int64
		found := false

		if obj.GetKind() == util.KindDaemonSet {
			replicas, found, errInternal = unstructured.NestedInt64(obj.Object, "status", "desiredNumberScheduled")
		} else {
			replicas, found, errInternal = unstructured.NestedInt64(obj.Object, "spec", "replicas")
		}
		if errInternal != nil || !found {
			return false, errInternal
		}

		if obj.GetKind() == util.KindDaemonSet {
			updatedReplicas, found, errInternal = unstructured.NestedInt64(obj.Object, "status", "updatedNumberScheduled")
		} else {
			updatedReplicas, found, errInternal = unstructured.NestedInt64(obj.Object, "status", "updatedReplicas")
		}
		if errInternal != nil || !found {
			return false, errInternal
		}

		if replicas == updatedReplicas {
			return true, nil
		}

		return false, nil
	})
}

// get the latest warning event for a workload controller
func (c *parentController) getLatestWarningEvents(namespace, name string) (bool, string) {
	// Get events that belong to the given workload controller
	events, err := c.clients.typedClient.CoreV1().Events(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return false, ""
	}
	var latestEnt apicorev1.Event
	visited := make(map[string]bool, 0)
	for _, ent := range events.Items {
		if ent.Type == apicorev1.EventTypeWarning && strings.HasPrefix(ent.InvolvedObject.Name, name) {
			if ent.CreationTimestamp.After(latestEnt.CreationTimestamp.Time) {
				latestEnt = ent
			}
			// Log all of non-duplicated warning events
			if visited[ent.Reason] {
				continue
			}
			visited[ent.Reason] = true
			glog.V(2).Infof("Found warning event on the workload controller %s/%s: %+v", namespace, name, ent.Message)

		}
	}
	if !latestEnt.CreationTimestamp.IsZero() {
		return true, latestEnt.Message
	}
	return false, ""
}

// Whether Operator controller should be skipped when executing a resize action on a K8s controller
// based on the label. If the SkipOperatorLabel is set to true on a K8s controller, resize action
// will directly update this controller regardless of upper Operator controller.
func (c *parentController) shouldSkipOperator(controller *unstructured.Unstructured) bool {
	labels := controller.GetLabels()
	if labels == nil {
		return false
	}
	labelVal, exists := labels[actionutil.SkipOperatorLabel]
	if exists && strings.EqualFold(labelVal, "true") {
		glog.Infof("Directly updating '%s %s/%s' regardless of Operator controller because '%s' label is set to true.",
			controller.GetKind(), controller.GetNamespace(), controller.GetName(), actionutil.SkipOperatorLabel)
		return true
	}
	return false
}

func (rc *parentController) String() string {
	return rc.name
}
