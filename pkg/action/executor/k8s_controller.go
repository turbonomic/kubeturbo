package executor

import (
	"fmt"

	apicorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
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
	replicas *int32
	podSpec  *apicorev1.PodSpec
}

type parentController struct {
	dynNamespacedClient dynamic.ResourceInterface
	obj                 *unstructured.Unstructured
	name                string
}

func (c *parentController) get(name string) (*k8sControllerSpec, error) {
	obj, err := c.dynNamespacedClient.Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	objName := fmt.Sprintf("%s/%s", obj.GetNamespace(), name)
	kind := obj.GetKind()

	replicas, found, err := unstructured.NestedInt64(obj.Object, "spec", "replicas")
	if err != nil || !found {
		return nil, fmt.Errorf("error retrieving replicas from %s %s: %v", kind, objName, err)
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
		replicas: &int32Replicas,
		podSpec:  &podSpec,
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

	if err := unstructured.SetNestedField(c.obj.Object, replicaVal, "spec", "replicas"); err != nil {
		return fmt.Errorf("error setting replicas into unstructured %s %s: %v", kind, objName, err)
	}
	if err := unstructured.SetNestedField(c.obj.Object, podSpecUnstructured, "spec", "template", "spec"); err != nil {
		return fmt.Errorf("error setting podSpec into unstructured %s %s: %v", kind, objName, err)
	}

	_, err = c.dynNamespacedClient.Update(c.obj, metav1.UpdateOptions{})
	return err
}

func (rc *parentController) String() string {
	return rc.name
}
