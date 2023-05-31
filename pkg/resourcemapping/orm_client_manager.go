package resourcemapping

import (
	"fmt"

	"github.com/golang/glog"
	discoveryutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	ormv2 "github.com/turbonomic/kubeturbo/pkg/resourcemapping/v2"
	devopsv1alpha1 "github.com/turbonomic/orm/api/v1alpha1"
	"github.com/turbonomic/orm/kubernetes"
	ormutils "github.com/turbonomic/orm/utils"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	restclient "k8s.io/client-go/rest"
)

// ORMClientManager defines top level object that will connect to the Kubernetes cluster
// to provide an interface to the kubeturbo ORM v1 and v2 resources.
type ORMClientManager struct {
	// Legacy v1 ORM client
	*ORMClient
	// v2 ORM client
	*ormv2.ORMv2Client
}

// New instance for ORMClientManager.
// dynamicClient is used to obtain ORM v1 resources.
// controller runtime client is used to obtain ORM v2 resources.
func NewORMClientManager(dynamicClient dynamic.Interface, kubeConfig *restclient.Config) *ORMClientManager {

	// ORM v1 client
	// TODO: Replace apiExtClient with runtimeClient
	apiExtClient, err := apiextclient.NewForConfig(kubeConfig)
	if err != nil {
		glog.Fatalf("Failed to generate apiExtensions client for kubernetes target: %v", err)
	}
	ormv1Client := NewORMClient(dynamicClient, apiExtClient)

	// ORM v2 client
	ormv2Client, err := ormv2.NewORMv2Client(kubeConfig)
	if err != nil {
		glog.Fatalf("Failed to create ORM v2 client: %v", err)
	}

	return &ORMClientManager{
		ormv1Client,
		ormv2Client,
	}
}

// Discover and cache ORMs v1 and v2.
func (manager *ORMClientManager) DiscoverORMs() {
	// ORM v1 are saved as a map in ORMClient
	manager.CacheORMSpecMap()
	numV1CRs := manager.CacheORMSpecMap()
	if numV1CRs > 0 {
		glog.Infof("Discovered %v v1 ORM Resources.", numV1CRs)
	}

	// ORM v2 are saved in a registry, see github.com/turbonomic/orm/registry/registry.go
	numV2CRs := manager.RegisterORMs()
	if numV2CRs > 0 {
		glog.Infof("Discovered %v v2 ORM Resources.", numV2CRs)
	}
}

type OwnerResources struct {
	ControllerObj *unstructured.Unstructured
	//---  V2 or V1
	// mapping of owner and the owner path for a given source/owned path
	OwnerResourcesMap map[string][]devopsv1alpha1.ResourcePath
	isV1ORM           bool
}

// This helper function will determine if the owner resources are found in orm V1, since we are returning the source/owned obj resourcepaths if it cannot
// locate owner paths. This will determine to check and log if it actually found the owners resources from v1 orm
func ownerResourcesFound(ownedObj *unstructured.Unstructured, allOwnerResourcePaths map[string][]devopsv1alpha1.ResourcePath) bool {
	for _, resourcePaths := range allOwnerResourcePaths {
		for _, resourcePath := range resourcePaths {
			if ownedObj.GetKind() != resourcePath.ObjectReference.Kind {
				return true
			}
		}
	}
	return false
}

// Return the owner resources to modify for a given source resource path
// to change the resources based an action recommendation from the Turbo server.
func (manager *ORMClientManager) GetOwnerResourcesForSource(ownedObj *unstructured.Unstructured,
	ownerReference discoveryutil.OwnerInfo, paths []string) (*OwnerResources, error) {
	var err error
	owned := createObjRef(ownedObj)
	allOwnerResourcePaths := make(map[string][]devopsv1alpha1.ResourcePath)
	//find ORM v2 given the source owned resource
	var sourceResourcePath []*devopsv1alpha1.ResourcePath
	foundORMV1 := false
	for _, path := range paths {
		var resourcePath *devopsv1alpha1.ResourcePath = &devopsv1alpha1.ResourcePath{
			ObjectReference: owned,
			Path:            path,
		}
		sourceResourcePath = append(sourceResourcePath, resourcePath)
		// If there are nested/hierarchy of owners, this always fetch the top owner resource. any updates on the top
		// owner resource will flow down the chain to get it's children to update
		ownerResourcePaths := manager.SeekTopOwnersResourcePathsForOwnedResourcePath(*resourcePath)
		// ownerResourcePaths will never be empty, as it returns the source/owned resource kind if it cannot
		// find the corresponding owner resources and it's paths. so check if the ownerResourcePaths returned
		// are same as resourcePath, if same then it didn't find any mappings related to V2 orm
		for _, ownerResourcePath := range ownerResourcePaths {
			if ownerResourcePath != *resourcePath {
				allOwnerResourcePaths[path] = ownerResourcePaths
			}
		}
	}

	if len(allOwnerResourcePaths) > 0 {
		glog.Infof("Found owner resource paths using ORM v2 for owned object %s:%s:%s",
			ownedObj.GetKind(), ownedObj.GetNamespace(), ownedObj.GetName())
	}
	// find legacy orm if cannot locate v2 orm
	if len(allOwnerResourcePaths) == 0 {
		allOwnerResourcePaths, err = manager.LocateOwnerPaths(ownedObj, ownerReference, sourceResourcePath)
		if err != nil {
			return &OwnerResources{
				ControllerObj:     ownedObj,
				OwnerResourcesMap: allOwnerResourcePaths,
				isV1ORM:           foundORMV1,
			}, err
		}
		// if it cannot locate owner resource path mappings from V1 orm, it returns the source/owned resource mapping path similar to above
		// V2 orm flow.
		if len(allOwnerResourcePaths) > 0 && ownerResourcesFound(ownedObj, allOwnerResourcePaths) {
			foundORMV1 = true
			glog.Infof("Found owner resource paths using ORM v1 for owned object %s:%s:%s",
				ownedObj.GetKind(), ownedObj.GetNamespace(), ownedObj.GetName())
		}
	}

	return &OwnerResources{
		ControllerObj:     ownedObj,
		OwnerResourcesMap: allOwnerResourcePaths,
		isV1ORM:           foundORMV1,
	}, nil

}

// UpdateOwners updates the corresponding owner CR for an owned manged resource
// updatedControllerObj -- updated K8s controller object based on Turbo actionItem, from which the resource value is fetched
//
//	and will be set to the corresponding CR
//
// controllerOwnerReference -- ownerReference of a K8s controller, which contains metadata of a owner CR
// ownerResources -- mapping of owner resource obj with owner resource path and source/owned controller obj
func (ormClient *ORMClientManager) UpdateOwners(updatedControllerObj *unstructured.Unstructured, controllerOwnerReference discoveryutil.OwnerInfo, ownerResources *OwnerResources) error {
	updated := false
	operatorResKind := controllerOwnerReference.Kind //operator kind and instance
	operatorResName := controllerOwnerReference.Name
	operatorRes := operatorResKind + "/" + operatorResName
	resourceNamespace := updatedControllerObj.GetNamespace()
	sourceResKind := updatedControllerObj.GetKind()
	sourceResName := updatedControllerObj.GetName()
	sourceRes := sourceResKind + "/" + sourceResName
	for ownedPath, resourcePaths := range ownerResources.OwnerResourcesMap {
		for _, resourcePath := range resourcePaths {
			// Retrieve the owner object and path
			ownerPath := resourcePath.Path
			ownerObj := resourcePath.ObjectReference
			ownerResKind := ownerObj.Kind
			ownerResName := ownerObj.Name
			ownerRes := ownerResKind + "/" + ownerResName
			ownerResNamespace := ownerObj.Namespace
			// ownerResources might have source/owned resource kind with their resource paths if it cannot find the owner resource mapping from ORM.
			// so we check if the owner kind and the contoller kind we get from action is same, in that case we cannot perform this update operation
			// on source/owned resource kind without owner resource found
			if ownerResKind == updatedControllerObj.GetKind() {
				glog.Warningf("owner resource not found for owned object: '%s' in namespace %s, skip updating owner CR",
					sourceRes, ownerResNamespace)
				continue
			}
			glog.Infof("Update owner %s/%s resource found for source %s/%s",
				ownerResKind, ownerResName,
				sourceResKind, sourceResName)
			ownerCR, err := kubernetes.Toolbox.GetResourceWithObjectReference(ownerObj)
			if err != nil {
				return fmt.Errorf("failed to get owner CR with owner object %s for %s in namespace %s: %v", ownerRes, sourceRes, ownerResNamespace, err)
			}
			// get the new resource value from the source obj
			newCRValue, found, err := ormutils.NestedField(updatedControllerObj.Object, ownedPath)
			if err != nil || !found {
				return fmt.Errorf("failed to get value for source/owned resource %s from path '%s' in action controller, error: %v", sourceRes, ownedPath, err)
			}
			// get the original resource value from the owner obj
			origCRValue, found, err := ormutils.NestedField(ownerCR.Object, ownerPath)
			if err != nil {
				return fmt.Errorf("failed to get value for owner resource %s from path '%s' in ownerCR for %s, error: %v", ownerRes, ownerPath, sourceRes, err)
			}
			if !found {
				glog.V(4).Infof("no values found for owner resource %s from path '%s' in ownerCR for %s",
					ownerRes, ownerPath, sourceRes)
			}
			// set new resource values to owenr cr obj
			if err := ormutils.SetNestedField(ownerCR.Object, newCRValue, ownerPath); err != nil {
				return fmt.Errorf("failed to set new value %v to owner CR %s at owner path '%s' for %s in namespace %s: %v",
					newCRValue, ownerRes, ownerPath, sourceRes, ownerResNamespace, err)
			}
			glog.V(2).Infof("updating owner resource %s for %s in namespace %s at owner path %s", ownerRes, sourceRes, ownerObj.Namespace, ownerPath)
			// update the owner cr object with new values set
			err = kubernetes.Toolbox.UpdateResourceWithGVK(ownerCR.GroupVersionKind(), ownerCR)
			if err != nil {
				return fmt.Errorf("failed to perform update action on owner CR %s for %s in namespace %s: %v", ownerRes, sourceRes, ownerResNamespace, err)
			}
			//set orm status only for owner object if this not orm V1
			if !ownerResources.isV1ORM {
				ormClient.SetORMStatusForOwner(ownerCR, nil)
			}
			updated = true
			glog.Infof("successfully updated owner CR %s for path '%s' from %v to %v for %s in namespace %s", ownerRes, ownerPath, origCRValue, newCRValue, sourceRes, ownerResNamespace)
		}
	}
	// If updated is false at this stage, it means there are some changes turbo server is recommending to make but not
	// defined in the ORM resource mapping templates. In this case, the resource field may be missing to be defined in
	// ORM CR so it couldn't find any owner resource paths to update.
	// We send an action failure notification here because nothing gets changes after the action execution.
	if !updated {
		return fmt.Errorf("failed to update owner CR %s for %s in namespace %s, missing owner resource", operatorRes, sourceRes, resourceNamespace)
	}
	return nil
}
