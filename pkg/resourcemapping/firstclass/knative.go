package firstclass

// Do not import kserve v1beta1 package to avoid the huge amount of packages introduced by kserve,
// Make unstructured ourselves.
import (
	"context"
	"fmt"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/orm/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var (
	knativeServiceGVK = schema.GroupVersionKind{
		Group:   "serving.knative.dev",
		Version: "v1",
		Kind:    "Service",
	}

	knativeMinScaleAnnotationKey = "autoscaling.knative.dev/min-scale"
	knativeMaxScaleAnnotationKey = "autoscaling.knative.dev/max-scale"

	knativeSpecTemplateMetadataAnnotationsPath = ".spec.template.metadata.annotations"

	// another way to find the mapping from kind to resource is detect during discovery and construct a global mapping tool
	knativeServiceGVR = schema.GroupVersionResource{
		Group:    knativeServiceGVK.Group,
		Version:  knativeServiceGVK.Version,
		Resource: "services",
	}

	knativeRevisionGVR = schema.GroupVersionResource{
		Group:    knativeServiceGVK.Group,
		Version:  knativeServiceGVK.Version,
		Resource: "revisions",
	}
	knativeRevisionKind = "Revision"

	knativeConfigurationsGVR = schema.GroupVersionResource{
		Group:    knativeServiceGVK.Group,
		Version:  knativeServiceGVK.Version,
		Resource: "configurations",
	}
	knativeConfigurationKind = "Configuration"

	kindToResourceMapping = map[string]schema.GroupVersionResource{
		knativeServiceGVK.Kind:   knativeServiceGVR,
		knativeRevisionKind:      knativeRevisionGVR,
		knativeConfigurationKind: knativeConfigurationsGVR,
	}
)

// there is case that customer don't want to scale service to 0, in this case we shall still set min/max both to 1
// this annotation is used to identify if the customer wants to enable/disable
var (
	knativeScaleToZeroAnnotationKey = "autoscaling.knative.dev/scale-to-zero-pod-retention-period"
)

type KNativeServiceOwner struct {
	dynamic.Interface
}

func newKNativeOwner(client dynamic.Interface) *KNativeServiceOwner {
	return &KNativeServiceOwner{
		Interface: client,
	}
}

// example of ownerreference chain in serverless mode
//   - apiVersion: apps/v1
//     kind: Deployment
//     name: granite-7b-lab-turbo-predictor-00001-deployment
//     |
//     V
//   - apiVersion: serving.knative.dev/v1
//     blockOwnerDeletion: true
//     controller: true
//     kind: Revision
//     name: granite-7b-lab-turbo-predictor-00001
//     uid: dfaea3ea-b58a-47c8-a492-1eaf35096523
//     |
//     V
//   - apiVersion: serving.knative.dev/v1
//     blockOwnerDeletion: true
//     controller: true
//     kind: Configuration
//     name: granite-7b-lab-turbo-predictor
//     uid: 04bf48c8-286b-4069-b2e8-ebb1f4ced90b
//     |
//     V
//   - apiVersion: serving.knative.dev/v1
//     blockOwnerDeletion: true
//     controller: true
//     kind: Service
//     name: granite-7b-lab-turbo-predictor
//     uid: 2ba2b65d-6ed1-4795-aac7-f4e8f68dc146
func (k *KNativeServiceOwner) getTrueOwnerReference(refIn metav1.OwnerReference, obj *unstructured.Unstructured) *metav1.OwnerReference {
	apiVersion, svckind := knativeServiceGVK.ToAPIVersionAndKind()
	ref := refIn

	var err error
	object := obj

	for ref.APIVersion == apiVersion {

		// The outer loop is trying to find the knative service from the ref chain.
		// if current ref is knative service, outer loop finishes.
		if ref.Kind == svckind {
			return &ref
		}

		// if current resource is not knative service, but from knative group (e.g. configuration or revision),
		// it obtain the object of that ref from k8s
		gvr, exists := kindToResourceMapping[ref.Kind]
		if !exists {
			return nil
		}
		object, err = k.Resource(gvr).Namespace(object.GetNamespace()).Get(context.TODO(), ref.Name, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("failed to get object for owner reference checking. error: %s, ref: %v", err.Error(), ref)
			return nil
		}
		refs := object.GetOwnerReferences()
		ref.APIVersion = ""
		// find for knative resource in owner list
		// this inner loop to move ref up in the chain.
		// The the updated ref is used in next outer round to check and move up.
		for _, r := range refs {
			if r.APIVersion == apiVersion {
				ref = r
				break
			}
		}
	}

	// If the knative group resource eventually is managed by something not knative service, then it is not knative service we support,
	// it leaves the outer loop and returns nil (means not the owner we support)

	glog.Infof("finished owner reference chain checking for object %s/%s, ownerrefs: %v it's not owned by knative", obj.GetNamespace(), obj.GetName(), obj.GetOwnerReferences())

	return nil
}

func (k *KNativeServiceOwner) getOwnerGroupVersionKind() schema.GroupVersionKind {

	return knativeServiceGVK
}

func (k *KNativeServiceOwner) getOwnerGroupVersionResource() schema.GroupVersionResource {

	return knativeServiceGVR
}

func (k *KNativeServiceOwner) replace(trueRef metav1.OwnerReference, owned *unstructured.Unstructured, ownedResourcePaths []string) (map[*unstructured.Unstructured][]string, error) {
	result := make(map[*unstructured.Unstructured][]string)
	paths := []string{}

	owner, err := k.Resource(knativeServiceGVR).Namespace(owned.GetNamespace()).Get(context.TODO(), trueRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	for _, path := range ownedResourcePaths {
		switch path {
		case deploymentReplicasPath:
			replicas, exists, err := utils.NestedField(owned.Object, path)
			if err != nil || !exists {
				glog.Errorf("failed to locate the replicas field in controller object. exists: %t, err: %v. Obj:%v", exists, err, owned)
				return nil, err
			}

			podTemplateSpec, resources, err := extractPodTemplateSpecAndAggregateResourceRequirements(owned)
			if err != nil {
				glog.Errorf("failed to aggregate resource requirements in pod template spec. error: %s, spec: %v", err.Error(), podTemplateSpec)
				return nil, err
			}

			// check namespace quota for rolling resource, we don't do the trick to temporary change the quota here as it is too risky for GPU
			if err = checkNamespaceQuotaForResources(k.Interface, owned.GetNamespace(), resources, replicas.(int64)); err != nil {
				glog.Error("failed in checking namespace quota. error: %s, namespace: %s resource:%d, replicas:%d", err.Error(), owned.GetNamespace(), resources, replicas.(int))
				return nil, err
			}

			selectors := aggregatedNodeSelectorInDeployment(podTemplateSpec)
			if err = checkResourceForRolling(k.Interface, selectors, resources, int(replicas.(int64))); err != nil {
				glog.Error("failed in checking resource for rolling. error: %s, resources: %v ", err.Error(), resources)
				return nil, err
			}

			// in order to work with other autoscaler to support 0<->1 replicas, we set the minReplicas to 0 when desired replica number is 1.
			minReplicas := replicas
			if minReplicas.(int64) == 1 && k.isScalingToZeroEnabledOnKNativeService(owned) {
				glog.V(2).Infof("Found desired replica = 1, and scale to 0 is enabled. Instead of setting min = max, set the minReplicas to 0 to work with other auto scaler to support 0<->1. object: %v", owner)
				minReplicas = 0
			}

			maxStr := fmt.Sprintf("%d", replicas)
			minStr := fmt.Sprintf("%d", minReplicas)
			paths = append(paths, knativeSpecTemplateMetadataAnnotationsPath)

			anno, exists, err := utils.NestedField(owner.Object, knativeSpecTemplateMetadataAnnotationsPath)
			if err != nil {
				glog.Errorf("error in locating the metadata.annotation field in controller object. exists: %t, err: %s. Obj:%v", exists, err.Error(), owner)
				continue
			}
			var annotations map[string]interface{}
			if exists {
				annotations = anno.(map[string]interface{})
			} else {
				annotations = make(map[string]interface{})
			}
			annotations[knativeMaxScaleAnnotationKey] = maxStr
			annotations[knativeMinScaleAnnotationKey] = minStr
			utils.SetNestedField(owner.Object, annotations, knativeSpecTemplateMetadataAnnotationsPath)
			break
		}
	}

	if len(paths) > 0 {
		result[owner] = paths
	}

	if len(result) == 0 {
		result[owned] = ownedResourcePaths
	}
	return result, nil

}

func (k *KNativeServiceOwner) isScalingToZeroEnabledOnKNativeService(obj *unstructured.Unstructured) bool {
	if obj == nil {
		return false
	}

	annotations := obj.GetAnnotations()
	if len(annotations) == 0 {
		return false
	}

	if _, exists := annotations[knativeScaleToZeroAnnotationKey]; !exists {
		return false
	}

	return true
}
