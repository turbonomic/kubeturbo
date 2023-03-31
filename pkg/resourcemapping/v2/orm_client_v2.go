package resourcemapping

import (
	"context"
	"fmt"
	"github.com/golang/glog"
	devopsv1alpha1 "github.com/turbonomic/orm/api/v1alpha1"

	"github.com/turbonomic/orm/api/v1alpha1"
	"github.com/turbonomic/orm/kubernetes"
	"github.com/turbonomic/orm/registry"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

func NewORMv2Client(kubeconfig *restclient.Config) (*ORMv2Client, error) {
	err := kubernetes.InitToolbox(kubeconfig, scheme)
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
		glog.Errorf("Unable to list orm list resource: %++v", err)
		return 0
	}

	ormClient.registerAllORMs(ormList.Items)
	return len(ormList.Items)
}

// Used for unit testing
func (ormClient *ORMv2Client) registerAllORMs(orms []v1alpha1.OperatorResourceMapping) {
	for _, orm := range orms {
		if &orm != nil {
			glog.V(2).Infof("****** ORM %v:%v", orm.Namespace, orm.Name)
			e := ormClient.registerORMWithStatus(&orm)
			if e != nil {
				glog.Errorf("Unable to register v2 orm %v/%v due to error: %v.",
					orm.Namespace, orm.Name, e)
				continue
			}
		}
	}
	return
}

// Register ORM and update the status with the parsed owner and owned paths
func (ormClient *ORMv2Client) registerORMWithStatus(orm *devopsv1alpha1.OperatorResourceMapping) error {
	var err error

	// Register ORM if owner exists
	err = ormClient.ValidateAndRegisterORM(orm)
	if err != nil {
		return err
	}
	glog.V(3).Infof("Registered orm %v:%v", orm.Namespace, orm.Name)

	var ownerObj *unstructured.Unstructured
	ownerObj, err = ormClient.getOwnerFromORM(orm)
	if err != nil {
		glog.Infof("Cannot update orm status, %++v", err)
		return err
	}

	if ownerObj == nil {
		glog.Infof("Cannot update orm status, null owner")
		return fmt.Errorf("Cannot update orm status, null owner")
	}

	ormClient.SetORMStatusForOwner(ownerObj, orm)

	return err
}

func (ormClient *ORMv2Client) getOwnerFromORM(orm *devopsv1alpha1.OperatorResourceMapping) (*unstructured.Unstructured, error) {
	var err error
	if orm == nil {
		return nil, nil
	}

	// get owner
	var obj *unstructured.Unstructured
	if orm.Spec.Owner.Name != "" {
		objk := types.NamespacedName{
			Namespace: orm.Spec.Owner.Namespace,
			Name:      orm.Spec.Owner.Name,
		}
		if objk.Namespace == "" {
			objk.Namespace = orm.Namespace
		}
		obj, err = kubernetes.Toolbox.GetResourceWithGVK(orm.Spec.Owner.GroupVersionKind(), objk)
		if err != nil {
			return nil, fmt.Errorf("failed to find owner %++v", orm.Spec.Owner)
		}
	} else {
		objs, err := kubernetes.Toolbox.GetResourceListWithGVKWithSelector(orm.Spec.Owner.GroupVersionKind(),
			types.NamespacedName{Namespace: orm.Spec.Owner.Namespace, Name: orm.Spec.Owner.Name}, &orm.Spec.Owner.LabelSelector)
		if err != nil || len(objs) == 0 {
			return nil, fmt.Errorf("failed to find owner %++v", orm.Spec.Owner)
		}
		obj = &objs[0]
	}

	orm.Spec.Owner.Name = obj.GetName()
	orm.Spec.Owner.Namespace = obj.GetNamespace()

	return obj, nil
}
