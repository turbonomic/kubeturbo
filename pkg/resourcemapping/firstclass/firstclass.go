package firstclass

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

type FirstClassOwner interface {
	replace(trueRef metav1.OwnerReference, obj *unstructured.Unstructured, ownedResourcePaths []string) (map[*unstructured.Unstructured][]string, error)
	getTrueOwnerReference(ref metav1.OwnerReference, obj *unstructured.Unstructured) *metav1.OwnerReference
	getOwnerGroupVersionKind() schema.GroupVersionKind
	getOwnerGroupVersionResource() schema.GroupVersionResource
}

type firstClassRegistry map[schema.GroupVersionKind]FirstClassOwner

type FirstClasssClient struct {
	registry firstClassRegistry
	client   dynamic.Interface
}

var (
	deploymentGVK = schema.GroupVersionKind{
		Group:   "apps",
		Version: "v1",
		Kind:    "Deployment",
	}
	deploymentReplicasPath    = ".spec.replicas"
	controllerPodTemplatePath = ".spec.template"
)

// pass back original obj and paths if not owned by first class citizens
func (f *FirstClasssClient) CheckAndReplaceWithFirstClassOwners(obj *unstructured.Unstructured, ownedResourcePaths []string) (map[*unstructured.Unstructured][]string, error) {

	result := make(map[*unstructured.Unstructured][]string)
	// start with itself
	result[obj] = ownedResourcePaths

	done := false
	// repeatedly check firstclass owner of firstclass owner
	for !done {
		round := make(map[*unstructured.Unstructured][]string)

		for o, p := range result {
			fco, err := f.identifyFirstClassOwners(o)
			if err != nil {
				return nil, err
			}

			if len(fco) > 0 {
				// if there is firstclass owner(s), do replace and add them to round result
				// we support the case of multiple owners
				for ref, gvk := range fco {
					objMap, err := f.registry[gvk].replace(ref, o, p)
					if err != nil {
						return nil, err
					}
					if len(objMap) > 0 {
						for k, v := range objMap {
							round[k] = v
						}
					}
				}
			} else {
				// otherwise put the object back
				round[o] = p
			}

			// technically, there is chance that owner chain diverge and then converge, even with different sub-chain length
			// example:
			//     /-- res2 -- res3 \
			// res1                  res5
			//     \---- res4 ----  /
			// we don't support this case yet
		}
		done = true
		if len(result) != len(round) {
			done = false
		} else {
			for k, v := range result {
				if !reflect.DeepEqual(round[k], v) {
					done = false
					break
				}
			}
		}

		result = round
	}

	return result, nil
}

func (f *FirstClasssClient) identifyFirstClassOwners(obj *unstructured.Unstructured) (map[metav1.OwnerReference]schema.GroupVersionKind, error) {

	refs := obj.GetOwnerReferences()
	if len(refs) == 0 {
		return nil, nil
	}

	result := make(map[metav1.OwnerReference]schema.GroupVersionKind)
	for _, ref := range refs {
		for _, o := range f.registry {
			trueRef := o.getTrueOwnerReference(ref, obj)
			if trueRef != nil {
				result[*trueRef] = o.getOwnerGroupVersionKind()
			}
		}
		// skip the ref if it does not belong to any firstclass owner
	}

	return result, nil
}

func NewFirstClassClient(kubeconfig *rest.Config) (*FirstClasssClient, error) {

	client, err := dynamic.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	reg := firstClassRegistry{
		kserveInferenceServiceGVK: newKServeInferenceServiceOwner(client),
		knativeServiceGVK:         newKNativeOwner(client),
	}

	return &FirstClasssClient{
		registry: reg,
		client:   client,
	}, nil
}
