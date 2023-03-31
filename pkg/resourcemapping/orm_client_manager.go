package resourcemapping

import (
	"github.com/golang/glog"
	ormv2 "github.com/turbonomic/kubeturbo/pkg/resourcemapping/v2"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
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
