package firstclass

// Do not import kserve v1beta1 package to avoid the huge amount of packages introduced by kserve,
// Make unstructured ourselves.
import (
	"strings"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.ibm.com/turbonomic/orm/utils"
)

var (
	inferenceServiceAPIVersion = "serving.kserve.io/v1beta1"
	inferenceServiceKind       = "InferenceService"

	inferenceServiceFieldSpec        = "spec"
	inferenceServiceFieldMinReplicas = "minReplicas"
	inferenceServiceFieldMaxReplicas = "maxReplicas"
	inferenceServiceNameSplit        = "-"

	inferenceServiceDeploymentReplicasPath = ".spec.replicas"
)

//  1. construct the inferenceservice owner obj with unstructured.Unstructured instead of import schema from kserve v1beta1
//     to avoid conflicts and risks with all the modules introduced with kserve
//  2. only construct the fields need to be changed and the path to those fields
//     the update action in k8s_controller/orm (pc.ormClient.UpdateOwners()) will get the object from k8s, update the field pointed by the path and then update object
func checkAndReplaceWithInferenceService(obj *unstructured.Unstructured, ownedResourcePaths []string) map[*unstructured.Unstructured][]string {

	result := make(map[*unstructured.Unstructured][]string)

	ownerReferences := obj.GetOwnerReferences()

	if len(ownerReferences) == 0 {
		result[obj] = ownedResourcePaths
		return result
	}

	for _, ref := range ownerReferences {
		if ref.APIVersion == inferenceServiceAPIVersion && ref.Kind == inferenceServiceKind {
			component := getComponentNameFromObject(obj, ref)
			if component == "" {
				continue
			}

			firstClassObj := prepareFirstClassObject(ref, obj.GetNamespace())
			paths := []string{}

			for _, path := range ownedResourcePaths {
				if path == inferenceServiceDeploymentReplicasPath {
					replicas, exists, err := utils.NestedField(obj.Object, path)
					if err != nil || !exists {
						glog.V(2).Infof("failed to locate the replicas field in controller object. exists: %t, err: %s. Obj:%v", exists, err.Error(), obj)
						continue
					}
					paths = append(paths, "."+inferenceServiceFieldSpec+"."+component+"."+inferenceServiceFieldMinReplicas)
					paths = append(paths, "."+inferenceServiceFieldSpec+"."+component+"."+inferenceServiceFieldMaxReplicas)

					firstClassObj.Object[inferenceServiceFieldSpec] = map[string]interface{}{
						component: map[string]interface{}{
							inferenceServiceFieldMinReplicas: replicas,
							inferenceServiceFieldMaxReplicas: replicas,
						},
					}
					glog.V(2).Infof("replacing owned resource with first class citizen InferenceService %v", firstClassObj)
				}
			}

			if len(paths) > 0 {
				result[firstClassObj] = paths
			}
		}
	}

	if len(result) == 0 {
		result[obj] = ownedResourcePaths
	}

	return result
}

var (
	inferenceServiceComponentLabelName = "component"
)

func getComponentNameFromObject(obj *unstructured.Unstructured, ownerRef metav1.OwnerReference) string {

	var name string

	// try to get from labels
	labels := obj.GetLabels()
	if len(labels) > 0 {
		if name, ok := labels[inferenceServiceComponentLabelName]; ok {
			return name
		}
	}

	// otherwise extract from object name
	// naming conversion: inferenceservicename-<component> or inferenceservicename-<component>-0000x-deployment
	objName := obj.GetName()
	ownerName := ownerRef.Name

	if len(objName) < len(ownerName) {
		return ""
	}

	name = objName[len(ownerName)+1:]
	return strings.Split(name, inferenceServiceNameSplit)[0]
}
