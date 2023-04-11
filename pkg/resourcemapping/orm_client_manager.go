package resourcemapping

import (
	"github.com/golang/glog"
	discoveryutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	ormv2 "github.com/turbonomic/kubeturbo/pkg/resourcemapping/v2"
	devopsv1alpha1 "github.com/turbonomic/orm/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
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
	OwnerResources map[string][]devopsv1alpha1.ResourcePath
	ErrorMsg       error
}

// Return the owner resources to modify for a given source resource path
// to change the resources based an action recommendation from the Turbo server.
func (manager *ORMClientManager) GetOwnerResourcesForSource(ownedObj *unstructured.Unstructured,
	ownerReference discoveryutil.OwnerInfo, paths []string) (*OwnerResources, error) {
	var err error
	var owned corev1.ObjectReference
	owned = corev1.ObjectReference{
		Kind:       ownedObj.GetKind(),
		Namespace:  ownedObj.GetNamespace(),
		Name:       ownedObj.GetName(),
		APIVersion: ownedObj.GetAPIVersion(),
	}
	var allOwnerResourcePaths map[string][]devopsv1alpha1.ResourcePath

	//find ORM v2 given the source owned resource
	var sourceResourcePath []*devopsv1alpha1.ResourcePath
	for _, path := range paths {
		var resourcePath *devopsv1alpha1.ResourcePath
		resourcePath = &devopsv1alpha1.ResourcePath{
			ObjectReference: owned,
			Path:            path,
		}
		sourceResourcePath = append(sourceResourcePath, resourcePath)
		ownerResourcePaths := manager.SeekTopOwnersResourcePathsForOwnedResourcePath(*resourcePath)
		if len(ownerResourcePaths) > 0 {
			allOwnerResourcePaths[path] = ownerResourcePaths
		}
	}

	if len(allOwnerResourcePaths) > 0 {
		glog.Infof("Found owner resource paths using ORM v2 for owned object %s:%s:%s",
			ownedObj.GetKind(), ownedObj.GetNamespace(), ownedObj.GetName())
	}
	// find legacy orm if cannot locate v2 orm
	if len(allOwnerResourcePaths) == 0 {
		allOwnerResourcePaths, err = manager.LocateOwnerPaths(ownedObj, ownerReference, sourceResourcePath)

		if len(allOwnerResourcePaths) > 0 {
			glog.Infof("Found owner resource paths using ORM v1 for owned object %s:%s:%s",
				ownedObj.GetKind(), ownedObj.GetNamespace(), ownedObj.GetName())
		}
	}

	return &OwnerResources{
		ControllerObj:  ownedObj,
		OwnerResources: allOwnerResourcePaths,
		ErrorMsg:       err,
	}, nil

}
