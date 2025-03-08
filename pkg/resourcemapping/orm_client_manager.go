package resourcemapping

import (
	"fmt"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/orm/api/v1alpha1"
	"github.ibm.com/turbonomic/orm/kubernetes"
	"github.ibm.com/turbonomic/orm/utils"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/resourcemapping/firstclass"
	v2 "github.ibm.com/turbonomic/kubeturbo/pkg/resourcemapping/v2"
)

// ORMClientManager defines top level object that will connect to the Kubernetes cluster
// to provide an interface to the ORM v1 and v2 resources.
type ORMClientManager struct {
	// Legacy v1 ORM client
	*ORMClient
	// v2 ORM client
	*v2.ORMv2Client
	// first class owners
	*firstclass.FirstClasssClient
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

	// First class client
	fcClient, err := firstclass.NewFirstClassClient(kubeConfig)
	if err != nil {
		glog.Fatalf("Failed to create firstclass client: %v", err)
	}

	return &ORMClientManager{
		v1Client,
		v2Client,
		fcClient,
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
		// Added special case for first class citizen where the owner reference is empty, put the path back
		for _, ownerResourcePath := range ownerResourcePathObjs {
			if ownerResourcePath != *ownedResourcePathObj || len(ownedObj.GetOwnerReferences()) == 0 {
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

	type updateItem struct {
		ownedPath string
		ownerPath string
	}

	objQueue := make(map[corev1.ObjectReference][]updateItem)

	for ownedPath, resourcePaths := range ownerResources.OwnerResourcesMap {

		glog.V(4).Infof("Going through ownedPath: %v and resourcePaths: %v", ownedPath, resourcePaths)

		for _, resourcePath := range resourcePaths {
			// Retrieve the owner object and path
			ownerPath := resourcePath.Path
			ownerObj := resourcePath.ObjectReference
			ownerResKind := ownerObj.Kind
			ownerResName := ownerObj.Name
			ownerResNamespace := ownerObj.Namespace
			// The ownerResources might have owned resource kind as their resource paths if it cannot find the owner
			// resource mapping from ORM.
			// We check if the owner kind and the controller kind we get from the action are the same, in which case
			// we cannot perform this update operation on owned resource kind without owner resource found.
			if ownerResKind == updatedControllerObj.GetKind() && updatedControllerObj.GetUID() != "" {
				// we still want to try to update the owner resource even it is the same for first class citizen.
				// for first class citizen, a new object is constructed, but not get from cluster, that object does not have a UID
				// in this case, want to apply the value from constructed object to actual object
				glog.Infof("owner resource is the same to owned object: '%s' in namespace %s, and it is not firstclass use case, uid: %s, skip updating owner",
					sourceRes, ownerResNamespace, updatedControllerObj.GetUID())
				continue
			}
			glog.V(4).Infof("Update owner %s/%s resource found for source %s/%s",
				ownerResKind, ownerResName,
				sourceResKind, sourceResName)

			// instead of updating the value immediately, enqueue it to merge the changes to same object
			item := updateItem{
				ownedPath: ownedPath,
				ownerPath: ownerPath,
			}

			items := objQueue[ownerObj]
			if len(items) == 0 {
				items = []updateItem{item}
			} else {
				items = append(items, item)
			}
			objQueue[ownerObj] = items

		}
	}

	// process everything here from the queue
	for obj, items := range objQueue {
		// get the objet again, in case there was changes from other source
		ownerCR, err := kubernetes.Toolbox.GetResourceWithObjectReference(obj)
		if err != nil {
			return fmt.Errorf("failed to get owner CR with owner object %v, err: %s", obj, err)
		}

		for _, item := range items {
			// get the new resource value from the source obj
			newCRValue, found, err := utils.NestedField(updatedControllerObj.Object, item.ownedPath)
			if err != nil || !found {
				return fmt.Errorf("failed to get value for source/owned resource %v from path '%s' in action controller, error: %v", updatedControllerObj, item.ownedPath, err)
			}

			// get the original resource value from the owner obj
			origCRValue, found, err := utils.NestedField(ownerCR.Object, item.ownerPath)
			if err != nil {
				return fmt.Errorf("failed to get value for owner resource %v from path '%s' err: %v", ownerCR, item.ownerPath, err)
			}

			if !found {
				glog.V(2).Infof("no values found for owner resource %v from path '%s'",
					obj, item.ownedPath)
			}

			// set new resource values to owner cr obj
			if err := utils.SetNestedField(ownerCR.Object, newCRValue, item.ownerPath); err != nil {
				return fmt.Errorf("failed to set new value %v to owner CR %sv at owner path '%s', err: %s",
					newCRValue, obj, item.ownerPath, err)
			}

			glog.V(2).Infof("Successfully replaced value for owner in path '%s' from %v to %v. owner: %v",
				item.ownerPath, origCRValue, newCRValue, obj)

		}
		// update the owner cr object with new values set
		err = kubernetes.Toolbox.UpdateResourceWithGVK(ownerCR.GroupVersionKind(), ownerCR)
		if err != nil {
			return fmt.Errorf("failed to perform update action on owner CR %v, err: %s", ownerCR, err)
		}

		glog.V(2).Infof("Updating owner resource %v ", obj)
		// set orm status only for owner object if this not orm V1
		// also check UID to see if it is a firstclass citizen case
		if !ownerResources.isV1ORM && updatedControllerObj.GetUID() != "" {
			ormClientMgr.SetORMStatusForOwner(ownerCR, nil)
		}

		updated = true
		glog.V(2).Infof("Successfully updated owner CR %v", obj)
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

// Checks for an Operator Resource Mapping
// NOTE: Will only support ORM V2+ for this
func (ormClientMgr *ORMClientManager) HasORM(controller *repository.K8sController) bool {
	ormv2 := ormClientMgr.ORMv2Client.ResourceMappingRegistry.RetrieveORMEntryForOwned(controller.ObjectReference())
	return ormv2 != nil
}

func (ormClientMgr *ORMClientManager) CheckAndReplaceWithFirstClassOwners(obj *unstructured.Unstructured, ownedResourcePaths []string) (map[*unstructured.Unstructured][]string, error) {
	return ormClientMgr.FirstClasssClient.CheckAndReplaceWithFirstClassOwners(obj, ownedResourcePaths)
}

type ORMHandler interface {
	// add this for compatibility, should merge this function into other 2 in separate issue
	CheckAndReplaceWithFirstClassOwners(obj *unstructured.Unstructured, ownedResourcePaths []string) (map[*unstructured.Unstructured][]string, error)

	HasORM(controller *repository.K8sController) bool

	GetOwnerResourcesForOwnedResources(
		ownedObj *unstructured.Unstructured,
		ownerReference util.OwnerInfo,
		ownedResourcePaths []string,
	) (*OwnerResources, error)
	DiscoverORMs()
}
