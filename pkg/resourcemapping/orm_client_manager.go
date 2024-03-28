package resourcemapping

import (
	"fmt"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/orm/api/v1alpha1"
	"github.ibm.com/turbonomic/orm/kubernetes"
	"github.ibm.com/turbonomic/orm/utils"
	v1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	v2 "github.ibm.com/turbonomic/kubeturbo/pkg/resourcemapping/v2"
)

// ORMClientManager defines top level object that will connect to the Kubernetes cluster
// to provide an interface to the ORM v1 and v2 resources.
type ORMClientManager struct {
	// Legacy v1 ORM client
	*ORMClient
	// v2 ORM client
	*v2.ORMv2Client
}

// NewORMClientManager creates new instance for ORMClientManager.
// dynamicClient is used to obtain ORM v1 resources.
// controller runtime client is used to obtain ORM v2 resources.
func NewORMClientManager(dynamicClient dynamic.Interface, kubeConfig *rest.Config) *ORMClientManager {
	// ORM v1 client
	// TODO: Replace apiExtClient with runtimeClient
	apiExtClient, err := v1.NewForConfig(kubeConfig)
	if err != nil {
		glog.Fatalf("Failed to generate apiExtensions client for kubernetes target: %v", err)
	}
	v1Client := NewORMClient(dynamicClient, apiExtClient)

	// ORM v2 client
	v2Client, err := v2.NewORMv2Client(kubeConfig)
	if err != nil {
		glog.Fatalf("Failed to create ORM v2 client: %v", err)
	}

	return &ORMClientManager{
		v1Client,
		v2Client,
	}
}

// DiscoverORMs discovers and caches ORMs v1 and v2.
func (ormClientMgr *ORMClientManager) DiscoverORMs() {
	// ORM v1 are saved as a map in ORMClient
	numV1CRs := ormClientMgr.CacheORMSpecMap()
	if numV1CRs > 0 {
		glog.Infof("Discovered %v v1 ORM Resources.", numV1CRs)
	}

	// ORM v2 are saved in a registry, see github.ibm.com/turbonomic/orm/registry/registry.go
	numV2CRs := ormClientMgr.RegisterORMs()
	if numV2CRs > 0 {
		glog.Infof("Discovered %v v2 ORM Resources.", numV2CRs)
	}
}

type OwnerResources struct {
	ControllerObj *unstructured.Unstructured
	//---  V2 or V1
	// mapping of owner and the owner path for a given source/owned path
	OwnerResourcesMap map[string][]v1alpha1.ResourcePath
	isV1ORM           bool
}

// GetOwnerResourcesForOwnedResources returns the owner resources to modify for a given source resource path
// to change the resources based an action recommendation from the Turbo server.
func (ormClientMgr *ORMClientManager) GetOwnerResourcesForOwnedResources(ownedObj *unstructured.Unstructured,
	ownerReference util.OwnerInfo, ownedResourcePaths []string,
) (*OwnerResources, error) {
	var err error
	owned := createObjRef(ownedObj)
	allOwnerResourcePathObjs := make(map[string][]v1alpha1.ResourcePath)

	// Find ORM v2 given the source owned resource
	var ownedResourcePathObjs []*v1alpha1.ResourcePath
	for _, ownedResourcePath := range ownedResourcePaths {
		ownedResourcePathObj := &v1alpha1.ResourcePath{
			ObjectReference: owned,
			Path:            ownedResourcePath,
		}
		ownedResourcePathObjs = append(ownedResourcePathObjs, ownedResourcePathObj)
		// If there are nested/hierarchy of owners, this always fetch the top owner resource. any updates on the top
		// owner resource will flow down the chain to get its children to update.
		ownerResourcePathObjs := ormClientMgr.SeekTopOwnersResourcePathsForOwnedResourcePath(*ownedResourcePathObj)
		// The ownerResourcePaths will never be empty, as the SeekTopOwnersResourcePathsForOwnedResourcePath function
		// returns the owned resource kind if it cannot find the corresponding owner resources and their paths. So
		// check if the ownerResourcePaths returned are the same as resourcePath, if same then it didn't find any
		// mappings related to V2 ORM.
		for _, ownerResourcePath := range ownerResourcePathObjs {
			if ownerResourcePath != *ownedResourcePathObj {
				allOwnerResourcePathObjs[ownedResourcePath] = ownerResourcePathObjs
			}
		}
	}

	foundORMV1 := false
	if len(allOwnerResourcePathObjs) > 0 {
		glog.Infof("Found owner resource paths using ORM v2 for owned object %s:%s:%s",
			ownedObj.GetKind(), ownedObj.GetNamespace(), ownedObj.GetName())
	} else {
		// Find legacy orm v1 if orm v2 is empty
		glog.Infof("Cannot locate owner resource paths using ORM v2 for owned object %s:%s:%s. Try ORM v1 ...",
			ownedObj.GetKind(), ownedObj.GetNamespace(), ownedObj.GetName())
		allOwnerResourcePathObjs, err = ormClientMgr.LocateOwnerPaths(ownedObj, ownerReference, ownedResourcePathObjs)
		if err != nil {
			return nil, err
		}
		// if it cannot locate owner resource path mappings from V1 orm, it returns the source/owned resource mapping path similar to above
		// V2 orm flow.
		if ownerResourcesFoundForORMV1(ownedObj, allOwnerResourcePathObjs) {
			foundORMV1 = true
			glog.Infof("Found owner resource paths using ORM v1 for owned object %s:%s:%s",
				ownedObj.GetKind(), ownedObj.GetNamespace(), ownedObj.GetName())
		}
	}

	return &OwnerResources{
		ControllerObj:     ownedObj,
		OwnerResourcesMap: allOwnerResourcePathObjs,
		isV1ORM:           foundORMV1,
	}, nil
}

// ownerResourcesFoundForORMV1 determines if the owner resources are found in orm V1, since we are returning the owned obj
// resourcePaths if it cannot locate owner paths. This will check and log if it actually found the owners resources
// from v1 orm
func ownerResourcesFoundForORMV1(ownedObj *unstructured.Unstructured, allOwnerResourcePaths map[string][]v1alpha1.ResourcePath) bool {
	for _, resourcePaths := range allOwnerResourcePaths {
		for _, resourcePath := range resourcePaths {
			if ownedObj.GetKind() != resourcePath.ObjectReference.Kind {
				return true
			}
		}
	}
	return false
}

// UpdateOwners updates the corresponding owner CR for an owned manged resource
// updatedControllerObj -- updated K8s controller object based on Turbo actionItem, from which the resource value is fetched
//
//	and will be set to the corresponding CR
//
// controllerOwnerReference -- ownerReference of a K8s controller, which contains metadata of owner CR
// ownerResources -- mapping of owner resource obj with owner resource path and source/owned controller obj
func (ormClientMgr *ORMClientManager) UpdateOwners(updatedControllerObj *unstructured.Unstructured, controllerOwnerReference util.OwnerInfo, ownerResources *OwnerResources) error {
	updated := false
	operatorResKind := controllerOwnerReference.Kind // operator kind and instance
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
			// The ownerResources might have owned resource kind as their resource paths if it cannot find the owner
			// resource mapping from ORM.
			// We check if the owner kind and the controller kind we get from the action are the same, in which case
			// we cannot perform this update operation on owned resource kind without owner resource found.
			if ownerResKind == updatedControllerObj.GetKind() {
				glog.Warningf("owner resource not found for owned object: '%s' in namespace %s, skip updating owner CR",
					sourceRes, ownerResNamespace)
				continue
			}
			glog.V(4).Infof("Update owner %s/%s resource found for source %s/%s",
				ownerResKind, ownerResName,
				sourceResKind, sourceResName)
			ownerCR, err := kubernetes.Toolbox.GetResourceWithObjectReference(ownerObj)
			if err != nil {
				return fmt.Errorf("failed to get owner CR with owner object %s for %s in namespace %s: %v", ownerRes, sourceRes, ownerResNamespace, err)
			}
			// get the new resource value from the source obj
			newCRValue, found, err := utils.NestedField(updatedControllerObj.Object, ownedPath)
			if err != nil || !found {
				return fmt.Errorf("failed to get value for source/owned resource %s from path '%s' in action controller, error: %v", sourceRes, ownedPath, err)
			}
			// get the original resource value from the owner obj
			origCRValue, found, err := utils.NestedField(ownerCR.Object, ownerPath)
			if err != nil {
				return fmt.Errorf("failed to get value for owner resource %s from path '%s' in ownerCR for %s, error: %v", ownerRes, ownerPath, sourceRes, err)
			}
			if !found {
				glog.V(4).Infof("no values found for owner resource %s from path '%s' in ownerCR for %s",
					ownerRes, ownerPath, sourceRes)
			}
			// set new resource values to owner cr obj
			if err := utils.SetNestedField(ownerCR.Object, newCRValue, ownerPath); err != nil {
				return fmt.Errorf("failed to set new value %v to owner CR %s at owner path '%s' for %s in namespace %s: %v",
					newCRValue, ownerRes, ownerPath, sourceRes, ownerResNamespace, err)
			}
			glog.V(2).Infof("Updating owner resource %s for %s in namespace %s at owner path %s ...",
				ownerRes, sourceRes, ownerObj.Namespace, ownerPath)
			// update the owner cr object with new values set
			err = kubernetes.Toolbox.UpdateResourceWithGVK(ownerCR.GroupVersionKind(), ownerCR)
			if err != nil {
				return fmt.Errorf("failed to perform update action on owner CR %s for %s in namespace %s: %v", ownerRes, sourceRes, ownerResNamespace, err)
			}
			// set orm status only for owner object if this not orm V1
			if !ownerResources.isV1ORM {
				ormClientMgr.SetORMStatusForOwner(ownerCR, nil)
			}
			updated = true
			glog.V(2).Infof("Successfully updated owner CR %s for path '%s' from %v to %v for %s in namespace %s.",
				ownerRes, ownerPath, origCRValue, newCRValue, sourceRes, ownerResNamespace)
		}
	}
	// If updated is false at this stage, it means there are some changes turbo server is recommending to make but not
	// defined in the ORM resource mapping templates. In this case, the resource field may be missing to be defined in
	// ORM CR, so it couldn't find any owner resource paths to update.
	// We send an action failure notification here because nothing gets changes after the action execution.
	if !updated {
		return fmt.Errorf("failed to update owner CR %s for %s in namespace %s, missing owner resource", operatorRes, sourceRes, resourceNamespace)
	}
	return nil
}
