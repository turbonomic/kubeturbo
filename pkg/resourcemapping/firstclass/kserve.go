package firstclass

// Do not import kserve v1beta1 package to avoid the huge amount of packages introduced by kserve,
// Make unstructured ourselves.
import (
	"context"
	"fmt"
	"strconv"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.ibm.com/turbonomic/orm/utils"
)

var (
	inferenceServiceFieldSpec        = "spec"
	inferenceServiceFieldMinReplicas = "minReplicas"
	inferenceServiceFieldMaxReplicas = "maxReplicas"
	inferenceServiceNameSplit        = "-"

	kserveInferenceServiceGVK = schema.GroupVersionKind{
		Group:   "serving.kserve.io",
		Version: "v1beta1",
		Kind:    "InferenceService",
	}

	kserveInferenceServiceGVR = schema.GroupVersionResource{
		Group:    kserveInferenceServiceGVK.Group,
		Version:  kserveInferenceServiceGVK.Version,
		Resource: "inferenceservices",
	}
)

type KServeInferenceServiceOwner struct {
	dynamic.Interface
}

func newKServeInferenceServiceOwner(client dynamic.Interface) *KServeInferenceServiceOwner {
	return &KServeInferenceServiceOwner{
		Interface: client,
	}
}

// there are many level of owners between deployment and isvc in serverless mode, skip to the owner name
// - apiVersion: serving.kserve.io/v1beta1
//   blockOwnerDeletion: true
//   controller: true
//   kind: InferenceService
//   name: granite-7b-lab-turbo
//   uid: 319bfc43-8788-49f9-809f-6c07ee3ba924

func (k *KServeInferenceServiceOwner) getTrueOwnerReference(refIn metav1.OwnerReference, obj *unstructured.Unstructured) *metav1.OwnerReference {

	ref := refIn

	apiVersion, kind := kserveInferenceServiceGVK.ToAPIVersionAndKind()
	if ref.APIVersion == apiVersion && ref.Kind == kind {
		return &ref
	}

	return nil
}

func (k *KServeInferenceServiceOwner) getOwnerGroupVersionKind() schema.GroupVersionKind {
	return kserveInferenceServiceGVK
}

func (k *KServeInferenceServiceOwner) getOwnerGroupVersionResource() schema.GroupVersionResource {
	return kserveInferenceServiceGVR
}

//  1. construct the inferenceservice owner obj with unstructured.Unstructured instead of import schema from kserve v1beta1
//     to avoid conflicts and risks with all the modules introduced with kserve
//  2. only construct the fields need to be changed and the path to those fields
//     the update action in k8s_controller/orm (pc.ormClient.UpdateOwners()) will get the object from k8s, update the field pointed by the path and then update object
func (k *KServeInferenceServiceOwner) replace(trueRef metav1.OwnerReference, owned *unstructured.Unstructured, ownedResourcePaths []string) (map[*unstructured.Unstructured][]string, error) {

	owner, err := k.Resource(kserveInferenceServiceGVR).Namespace(owned.GetNamespace()).Get(context.TODO(), trueRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	component := getInferenceComponentNameFromObject(owned)
	if component == "" {
		return nil, fmt.Errorf("failed to find inference service component name in object: %s of %s", owned.GetNamespace()+"/"+owned.GetName(), owned.GroupVersionKind().String())
	}

	// two possible owned kind: deployment and knative service
	switch owned.GroupVersionKind() {
	case deploymentGVK:
		return k.replaceDeployment(owner, component, owned, ownedResourcePaths)
	case knativeServiceGVK:
		return k.replaceKNativeService(owner, component, owned, ownedResourcePaths)
	}

	return map[*unstructured.Unstructured][]string{
		owned: ownedResourcePaths,
	}, nil
}

func (k *KServeInferenceServiceOwner) replaceDeployment(owner *unstructured.Unstructured, component string, owned *unstructured.Unstructured, ownedResourcePaths []string) (map[*unstructured.Unstructured][]string, error) {
	result := make(map[*unstructured.Unstructured][]string)
	paths := []string{}

	for _, path := range ownedResourcePaths {
		switch path {

		case deploymentReplicasPath:
			replicas, exists, err := utils.NestedField(owned.Object, path)
			if err != nil || !exists {
				glog.V(2).Infof("failed to locate the replicas field in controller object. exists: %t, err: %s. Obj:%v", exists, err.Error(), owned)
				continue
			}
			paths = append(paths, "."+inferenceServiceFieldSpec+"."+component+"."+inferenceServiceFieldMinReplicas)
			paths = append(paths, "."+inferenceServiceFieldSpec+"."+component+"."+inferenceServiceFieldMaxReplicas)

			// in order to work with other autoscaler to support 0<->1 replicas, we set the minReplicas to 0 when desired replica number is 1.
			minReplicas := replicas
			if minReplicas.(int64) == 1 {
				glog.V(2).Infof("Found desired replica = 1, instead of setting min = max, set the minReplicas to 0 to work with other auto scaler to support 0<->1. object: %v", owner)
				minReplicas = 0
			}

			owner.Object[inferenceServiceFieldSpec] = map[string]interface{}{
				component: map[string]interface{}{
					inferenceServiceFieldMinReplicas: minReplicas,
					inferenceServiceFieldMaxReplicas: replicas,
				},
			}
			glog.V(2).Infof("replacing owned resource with first class citizen InferenceService %v", owner)
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

func (k *KServeInferenceServiceOwner) replaceKNativeService(owner *unstructured.Unstructured, component string, owned *unstructured.Unstructured, ownedResourcePaths []string) (map[*unstructured.Unstructured][]string, error) {
	result := make(map[*unstructured.Unstructured][]string)
	paths := []string{}

	for _, path := range ownedResourcePaths {
		switch path {
		case knativeSpecTemplateMetadataAnnotationsPath:
			anno, exists, err := utils.NestedField(owned.Object, path)
			if err != nil || !exists {
				glog.V(2).Infof("failed to locate the replicas field in controller object. exists: %t, err: %s. Obj:%v", exists, err.Error(), owned)
				continue
			}
			annotations := anno.(map[string]interface{})

			v := annotations[knativeMinScaleAnnotationKey]
			min, err := strconv.Atoi(v.(string))
			if err != nil {
				return nil, err
			}
			p := "." + inferenceServiceFieldSpec + "." + component + "." + inferenceServiceFieldMinReplicas
			paths = append(paths, p)
			utils.SetNestedField(owner.Object, min, p)

			v = annotations[knativeMaxScaleAnnotationKey]
			max, err := strconv.Atoi(v.(string))
			p = "." + inferenceServiceFieldSpec + "." + component + "." + inferenceServiceFieldMaxReplicas
			paths = append(paths, p)
			utils.SetNestedField(owner.Object, max, p)
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

var (
	inferenceServiceComponentLabelName = "component"
)

func getInferenceComponentNameFromObject(obj *unstructured.Unstructured) string {

	// try to get from labels
	labels := obj.GetLabels()
	if len(labels) > 0 {
		if name, ok := labels[inferenceServiceComponentLabelName]; ok {
			return name
		}
	}

	return ""
}
