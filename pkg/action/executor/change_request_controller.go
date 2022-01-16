package executor

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	util "github.com/turbonomic/kubeturbo/pkg/util"
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
	Update(replicas int64, podSpec map[string]interface{}) error
}

type cRControllerImpl struct {
	dynClient  dynamic.Interface
	obj        *unstructured.Unstructured
	managerApp *repository.K8sApp
}

func newCRController(dynClient dynamic.Interface, obj *unstructured.Unstructured, managerApp *repository.K8sApp) cRController {
	return &cRControllerImpl{
		dynClient:  dynClient,
		obj:        obj,
		managerApp: managerApp,
	}
}

func (cr *cRControllerImpl) Update(replicas int64, podSpec map[string]interface{}) error {
	path, repo, revision, err := cr.getFieldsFromManagerApp()
	if err != nil {
		return err
	}

	cR := newCRResource(cr.obj.GetName(), cr.obj.GetNamespace())
	err = unstructured.SetNestedField(cR.Object, "GitHub", "spec", "type")
	if err != nil {
		return err
	}
	err = unstructured.SetNestedField(cR.Object, repo, "spec", "source")
	if err != nil {
		return err
	}
	err = unstructured.SetNestedField(cR.Object, path, "spec", "path")
	if err != nil {
		return err
	}
	err = unstructured.SetNestedField(cR.Object, revision, "spec", "branch")
	if err != nil {
		return err
	}

	// The secret name and namspace if listed override the ones configured on the CR controller
	annotations := cr.obj.GetAnnotations()
	secretName, exists := annotations[GitopsSecretNameAnnotationKey]
	if exists {
		if err = unstructured.SetNestedField(cR.Object, secretName, "spec", "secretRef", "name"); err != nil {
			return err
		}
	}
	secretNamespace, exists := annotations[GitopsSecretNamespaceAnnotationKey]
	if exists {
		if err = unstructured.SetNestedField(cR.Object, secretNamespace, "spec", "secretRef", "namespace"); err != nil {
			return err
		}
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

		// TODO: Implement a wait loop here to retrieve the CR again and wait
		// until the status is either "Complete" or "Failed".
		// Once the retrieved status is complete, resetting the status to ""
		// and updating the spec would mean the CR controller will try to push
		// the changes again. We will also need to ensure that pushing the change
		// does not mean creating multiple PRs if the previous one exists.

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

func (c *cRControllerImpl) getFieldsFromManagerApp() (string, string, string, error) {
	if c.managerApp == nil {
		return "", "", "", fmt.Errorf("workload controller not managed by gitops pipeline: %s", c)
	}
	res := schema.GroupVersionResource{
		Group:    util.ArgoCDApplicationGV.Group,
		Version:  util.ArgoCDApplicationGV.Version,
		Resource: util.ApplicationResName,
	}

	app, err := c.dynClient.Resource(res).Namespace(c.managerApp.Namespace).Get(context.TODO(), c.managerApp.Name, metav1.GetOptions{})
	if err != nil {
		return "", "", "", fmt.Errorf("failed to get manager app for workload controller %s: %v", c, err)
	}

	var path, repo, revision string
	found := false
	path, found, err = unstructured.NestedString(app.Object, "spec", "source", "path")
	if err != nil || !found {
		return "", "", "", fmt.Errorf("required field path not found in manager app %s/%s: %v", app.GetNamespace(), app.GetName(), err)
	}
	repo, found, err = unstructured.NestedString(app.Object, "spec", "source", "repoURL")
	if err != nil || !found {
		return "", "", "", fmt.Errorf("required field repoURL not found in manager app %s/%s: %v", app.GetNamespace(), app.GetName(), err)
	}
	revision, found, err = unstructured.NestedString(app.Object, "spec", "source", "targetRevision")
	if err != nil || !found {
		return "", "", "", fmt.Errorf("required field targetRevision not found in manager app %s/%s: %v", app.GetNamespace(), app.GetName(), err)
	}

	return path, repo, revision, nil
}

func (c *cRControllerImpl) String() string {
	return fmt.Sprintf("%s/%s", c.obj.GetNamespace(), c.obj.GetName())
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
