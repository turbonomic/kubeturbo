package executor

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	CRGroup        = "turbonomic.io"
	CRVersion      = "v1alpha1"
	CRResourceName = "changerequests"
	CRKind         = "ChangeRequest"

	// We currently use the CR related values as an annotation on the
	// given workload controller. At some point we will need to move this
	// to a more structured place, ie. a custom resource.
	GitopsSourceAnnotationKey          = "turbonomic.io/gitops-source"
	GitopsFilePathAnnotationKey        = "turbonomic.io/gitops-file-path"
	GitopsBranchAnnotationKey          = "turbonomic.io/gitops-branch"
	GitopsSecretNameAnnotationKey      = "turbonomic.io/gitops-secret-name"
	GitopsSecretNamespaceAnnotationKey = "turbonomic.io/gitops-secret-namespace"
)

type cRController interface {
	// Update will create a ChangeRequest resource with
	// path from the obj annotation and payload as the
	// json representation of the obj
	// The creation of ChangeRequest will be observed by the
	// change reconciler which will update the source path
	// with the new object
	Update(sourcePath string, replicas int64, podSpec map[string]interface{}) error
}

type cRControllerImpl struct {
	dynClient dynamic.Interface
	obj       *unstructured.Unstructured
}

func newCRController(dynClient dynamic.Interface, obj *unstructured.Unstructured) cRController {
	return &cRControllerImpl{
		dynClient: dynClient,
		obj:       obj,
	}
}

func (cr *cRControllerImpl) Update(source string, replicas int64, podSpec map[string]interface{}) error {
	annotations := cr.obj.GetAnnotations()
	filePath, exists := annotations[GitopsFilePathAnnotationKey]
	if !exists {
		return fmt.Errorf("annotation %s needed for ChangeRequest creation does not exist", GitopsFilePathAnnotationKey)
	}
	branch, exists := annotations[GitopsBranchAnnotationKey]
	if !exists {
		return fmt.Errorf("annotation %s needed for ChangeRequest creation does not exist", GitopsBranchAnnotationKey)
	}
	secretName, exists := annotations[GitopsSecretNameAnnotationKey]
	if !exists {
		return fmt.Errorf("annotation %s needed for ChangeRequest creation does not exist", GitopsSecretNameAnnotationKey)
	}
	secretNamespace, exists := annotations[GitopsSecretNamespaceAnnotationKey]
	if !exists {
		return fmt.Errorf("annotation %s needed for ChangeRequest creation does not exist", GitopsSecretNamespaceAnnotationKey)
	}

	cR := newCRResource(cr.obj.GetName(), cr.obj.GetNamespace())
	err := unstructured.SetNestedField(cR.Object, "GitHub", "spec", "type")
	if err != nil {
		return err
	}
	err = unstructured.SetNestedField(cR.Object, source, "spec", "source")
	if err != nil {
		return err
	}
	err = unstructured.SetNestedField(cR.Object, filePath, "spec", "filePath")
	if err != nil {
		return err
	}
	err = unstructured.SetNestedField(cR.Object, branch, "spec", "branch")
	if err != nil {
		return err
	}
	err = unstructured.SetNestedField(cR.Object, secretName, "spec", "secretRef", "name")
	if err != nil {
		return err
	}
	err = unstructured.SetNestedField(cR.Object, secretNamespace, "spec", "secretRef", "namespace")
	if err != nil {
		return err
	}

	var patches []interface{}
	patches = append(patches, map[string]interface{}{
		"op":    "replace",
		"path":  "/spec/replicas",
		"value": replicas,
	})
	patches = append(patches, map[string]interface{}{
		"op":    "replace",
		"path":  "/spec/template/spec",
		"value": podSpec,
	})

	err = unstructured.SetNestedSlice(cR.Object, patches, "spec", "patchItems")
	if err != nil {
		return err
	}

	res := schema.GroupVersionResource{
		Group:    CRGroup,
		Version:  CRVersion,
		Resource: CRResourceName,
	}
	namespacedClient := cr.dynClient.Resource(res).Namespace(cr.obj.GetNamespace())

	// We try to create a new CR resource with the update info.
	// It might be that this action has been executed once and we are trying it
	// again in which case we try to only update the existing resource
	// Also we create the ChangeRequest resource with same name as workload obj
	_, outerErr := namespacedClient.Create(context.Background(), cR, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(outerErr) {
		cRSpec, found, err := unstructured.NestedMap(cR.Object, "spec")
		if err != nil || !found {
			return fmt.Errorf("could not retrieve spec fields from ChangeRequest: %s: %v", cR.GetName(), err)
		}

		existing, err := namespacedClient.Get(context.Background(), cR.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		err = unstructured.SetNestedMap(existing.Object, cRSpec, "spec")
		if err != nil {
			return err
		}

		// TODO: Reset the status too, because if the status still remains
		// 'completed' the change-reconciler would not do anything.
		// Brainstorm a little more on this.
		_, err = namespacedClient.Update(context.Background(), cR, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return outerErr
}

func (c *cRControllerImpl) String() string {
	return c.obj.GetName()
}

func newCRResource(name, namespace string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetKind(CRKind)
	gv := schema.GroupVersion{Group: CRGroup, Version: CRVersion}
	obj.SetAPIVersion(gv.String())
	obj.SetName(name)
	obj.SetNamespace(namespace)

	return obj
}
