package firstclass

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// general entry for all first-class resources, check them one by one
func CheckAndReplaceWithFirstClassOwners(obj *unstructured.Unstructured, ownedResourcePaths []string) map[*unstructured.Unstructured][]string {

	// when there are more than 1 first class citizen in the future, merge the map and return
	return checkAndReplaceWithInferenceService(obj, ownedResourcePaths)
}

func prepareFirstClassObject(ref metav1.OwnerReference, namespace string) *unstructured.Unstructured {
	firstClassObj := &unstructured.Unstructured{}

	firstClassObj.SetAPIVersion(ref.APIVersion)
	firstClassObj.SetKind(ref.Kind)
	firstClassObj.SetNamespace(namespace)
	firstClassObj.SetName(ref.Name)

	return firstClassObj
}
