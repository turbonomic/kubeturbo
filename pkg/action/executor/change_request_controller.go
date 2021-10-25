package executor

import (
	"context"
	"fmt"

	"github.com/ghodss/yaml"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	CRGroup        = "turbonomic.kubeturbo.io"
	CRVersion      = "v1alpha1"
	CRResourceName = "changerequests"
	CRKind         = "ChangeRequest"

	// We currently use the source of truth path as an annotation on the
	// given workload controller. At some point we will need to move this
	// to a more structured place, for example a CR.
	GitopsSourceAnnotationKey = "turbonomic.kubeturbo.io/gitops-source"
)

type cRController interface {
	// Update will create a ChangeRequest resource with
	// path from the obj annotation and payload as the
	// json representation of the obj
	// The creation of ChangeRequest will be observed by the
	// change reconciler which will update the source path
	// with the new object
	Update(sourcePath string, workloadObj *unstructured.Unstructured) error
}

type cRControllerImpl struct {
	dynClient dynamic.Interface
	name      string
}

func newCRController(dynClient dynamic.Interface) cRController {
	return &cRControllerImpl{
		dynClient: dynClient,
		name:      "ChangeRequestController",
	}
}

func (c *cRControllerImpl) Update(sourcePath string, workloadObj *unstructured.Unstructured) error {
	objJSON, err := workloadObj.MarshalJSON()
	if err != nil {
		return fmt.Errorf("Error encoding unstructured object to yaml: %v", err)
	}

	data, err := yaml.JSONToYAML(objJSON)
	if err != nil {
		return fmt.Errorf("Error encoding json object to yaml: %v", err)
	}

	cR := newCRResource(workloadObj.GetName(), workloadObj.GetNamespace())
	err = unstructured.SetNestedField(cR.Object, sourcePath, "spec", "pathname")
	if err != nil {
		return err
	}
	err = unstructured.SetNestedField(cR.Object, string(data), "spec", "payload")
	if err != nil {
		return err
	}
	err = unstructured.SetNestedField(cR.Object, "GitHub", "spec", "type")
	if err != nil {
		return err
	}

	res := schema.GroupVersionResource{
		Group:    CRGroup,
		Version:  CRVersion,
		Resource: CRResourceName,
	}
	namespacedClient := c.dynClient.Resource(res).Namespace(workloadObj.GetNamespace())

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

		_, err = namespacedClient.Update(context.Background(), cR, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	}

	return outerErr
}

func (c *cRControllerImpl) String() string {
	return c.name
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
