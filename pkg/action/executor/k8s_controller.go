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
	"k8s.io/client-go/dynamic"

	actionutil "github.com/turbonomic/kubeturbo/pkg/action/util"
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

type parentController struct {
	dynClient           dynamic.Interface
	dynNamespacedClient dynamic.ResourceInterface
	obj                 *unstructured.Unstructured
	name                string
	ormClient           *resourcemapping.ORMClient
}

func (c *parentController) get(name string) (*k8sControllerSpec, error) {
	obj, err := c.dynNamespacedClient.Get(context.TODO(), name, metav1.GetOptions{})
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

	annotations := c.obj.GetAnnotations()
	gitopsSource, exists := annotations[GitopsSourceAnnotationKey]
	if exists {
		// The workload is managed by a pipeline controller which replicates
		// it from a source of truth
		cRC := newCRController(c.dynClient)
		err := cRC.Update(gitopsSource, c.obj)
		if err != nil {
			return fmt.Errorf("failed to create a ChangeRequest: %v", err)
		}
		return nil
	}

	controllerOwnerReferences := c.obj.GetOwnerReferences()
	if !c.shouldSkipOperator(c.obj) && len(controllerOwnerReferences) > 0 && (*controllerOwnerReferences[0].Controller || "ClusterServiceVersion" == controllerOwnerReferences[0].Kind) {
		// If k8s controller is controlled by custom controller, update the CR using OperatorResourceMapping
		// if SkipOperatorLabel is not set or not true.
		if c.ormClient == nil {
			return fmt.Errorf("failed to execute action with nil ORMClient")
		}
		err = c.ormClient.Update(origControllerObj, c.obj, controllerOwnerReferences[0])
	} else {
		_, err = c.dynNamespacedClient.Update(context.TODO(), c.obj, metav1.UpdateOptions{})
	}
	return err
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
