package resourcemapping

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	devopsv1alpha1 "github.ibm.com/turbonomic/orm/api/v1alpha1"

	"github.ibm.com/turbonomic/orm/api/v1alpha1"
	"github.ibm.com/turbonomic/orm/kubernetes"
	"github.ibm.com/turbonomic/orm/registry"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	scheme = runtime.NewScheme()
)

var (
	listOptions = client.ListOptions{
		Namespace: v1.NamespaceAll,
	}
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

// ORMClient builds operator resource mapping templates fetched from OperatorResourceMapping CR in discovery client
// and provides the capability to update the corresponding Operator managed CR in action execution client.
type ORMv2Client struct {
	*registry.ResourceMappingRegistry
}

// TODO: Passing the event recorder as nil as of now, will have to come up with creating
// own event recorder during this initialization of toolbox to capture events
func NewORMv2Client(kubeconfig *restclient.Config) (*ORMv2Client, error) {
	err := kubernetes.InitToolbox(kubeconfig, scheme, nil)
	if err != nil {
		glog.Errorf("Unable to create orm v2 client: %++v", err)
		return nil, err
	}

	registry := &registry.ResourceMappingRegistry{}
	return &ORMv2Client{
		registry,
	}, nil
}

// Discover and register the v2 ORMs
func (ormClient *ORMv2Client) RegisterORMs() int {
	ormList := &v1alpha1.OperatorResourceMappingList{}
	err := kubernetes.Toolbox.OrmClient.List(context.TODO(), ormList, &listOptions)

	if err != nil {
		glog.Warningf("Could not list orm list resource: %++v", err)
		return 0
	}

	ormClient.registerAllORMs(ormList.Items)
	return len(ormList.Items)
}

func (ormClient *ORMv2Client) registerAllORMs(orms []v1alpha1.OperatorResourceMapping) {
	var registeredCnt int8
	for _, orm := range orms {
		if &orm != nil {
			e := ormClient.registerORMWithStatus(&orm)
			if e != nil {
				glog.Errorf("Unable to register v2 orm %v/%v due to error: %v.",
					orm.Namespace, orm.Name, e)
				continue
			}
			registeredCnt++
			glog.V(2).Infof("Registered ORM %v:%v", orm.Namespace, orm.Name)
		}
	}
	glog.V(2).Infof("Found %v orms, registered %v", len(orms), registeredCnt)
	return
}

// Register ORM and update the status with the parsed owner and owned paths
func (ormClient *ORMv2Client) registerORMWithStatus(orm *devopsv1alpha1.OperatorResourceMapping) error {
	var err error
	var ownerObj *unstructured.Unstructured
	// Register ORM if owner exists
	_, ownerObj, err = ormClient.ValidateAndRegisterORM(orm)
	if err != nil {
		return err
	}
	glog.V(3).Infof("Registered orm %v:%v", orm.Namespace, orm.Name)

	if ownerObj == nil {
		glog.Warningf("Cannot update orm status, null owner")
		return fmt.Errorf("Cannot update orm status, null owner")
	}

	// Set the state of the ORM with the list of owner resource paths and the current resource values
	ormClient.SetORMStatusForOwner(ownerObj, orm)

	return err
}
