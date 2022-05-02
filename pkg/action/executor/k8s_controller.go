package executor

import (
	"context"
	"fmt"
	"strings"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"

	apicorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/dynamic"
	typedClient "k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/action/executor/gitops"
	actionutil "github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/features"
	"github.com/turbonomic/kubeturbo/pkg/util"
)

// k8sController defines a common interface for kubernetes controller actions
// Currently supported controllers include:
// - ReplicationController
// - ReplicaSet
// - Deployment
type k8sController interface {
	get(name string) (*k8sControllerSpec, error)
	update(updatedSpec *k8sControllerSpec) error
}

// k8sControllerSpec defines a set of objects that we want to update:
// - replicas: The replicas of a controller to update for horizontal scale
// - podSpec: The pod template of a controller to update for consistent resize
// Note: Use pointer for in-place update
type k8sControllerSpec struct {
	replicas       *int32
	podSpec        *apicorev1.PodSpec
	controllerName string
}

type kubeClients struct {
	typedClient *typedClient.Clientset
	dynClient   dynamic.Interface
	// TODO: remove the need of this as we have dynClient already
	dynNamespacedClient dynamic.ResourceInterface
}

type parentController struct {
	clients    kubeClients
	obj        *unstructured.Unstructured
	name       string
	ormClient  *resourcemapping.ORMClient
	managerApp *repository.K8sApp
	gitConfig  gitops.GitConfig
}

func (c *parentController) get(name string) (*k8sControllerSpec, error) {
	obj, err := c.clients.dynNamespacedClient.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	objName := fmt.Sprintf("%s/%s", obj.GetNamespace(), name)
	kind := obj.GetKind()

	replicas := int64(0)
	found := false
	if kind != util.KindDaemonSet { // daemonsets do not have replica field
		replicas, found, err = unstructured.NestedInt64(obj.Object, "spec", "replicas")
		if err != nil || !found {
			return nil, fmt.Errorf("error retrieving replicas from %s %s: %v", kind, objName, err)
		}
	}

	podSpecUnstructured, found, err := unstructured.NestedFieldCopy(obj.Object, "spec", "template", "spec")
	if err != nil || !found {
		return nil, fmt.Errorf("error retrieving podSpec from %s %s: %v", kind, objName, err)
	}

	podSpec := apicorev1.PodSpec{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(podSpecUnstructured.(map[string]interface{}), &podSpec); err != nil {
		return nil, fmt.Errorf("error converting unstructured pod spec to typed pod spec for %s %s: %v", kind, objName, err)
	}

	c.obj = obj
	int32Replicas := int32(replicas)
	return &k8sControllerSpec{
		replicas:       &int32Replicas,
		podSpec:        &podSpec,
		controllerName: fmt.Sprintf("%s-%s", kind, objName),
	}, nil
}

func (c *parentController) update(updatedSpec *k8sControllerSpec) error {
	objName := fmt.Sprintf("%s/%s", c.obj.GetNamespace(), c.obj.GetName())
	kind := c.obj.GetKind()

	replicaVal := int64(*updatedSpec.replicas)
	podSpecUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(updatedSpec.podSpec)
	if err != nil {
		return fmt.Errorf("error converting pod spec to unstructured pod spec for %s %s: %v", kind, objName, err)
	}

	origControllerObj := c.obj.DeepCopy()
	if kind != util.KindDaemonSet { // daemonsets do not have replica field
		if err := unstructured.SetNestedField(c.obj.Object, replicaVal, "spec", "replicas"); err != nil {
			return fmt.Errorf("error setting replicas into unstructured %s %s: %v", kind, objName, err)
		}
	}
	if err := unstructured.SetNestedField(c.obj.Object, podSpecUnstructured, "spec", "template", "spec"); err != nil {
		return fmt.Errorf("error setting podSpec into unstructured %s %s: %v", kind, objName, err)
	}

	if c.managerApp != nil &&
		c.managerApp.Type != repository.AppTypeK8s &&
		utilfeature.DefaultFeatureGate.Enabled(features.GitopsApps) {
		var manager gitops.GitopsManager
		switch c.managerApp.Type {
		case repository.AppTypeArgoCD:
			// The workload is managed by a pipeline controller (argoCD) which replicates
			// it from a source of truth
			manager = gitops.NewGitHubManager(c.gitConfig, c.clients.typedClient, c.clients.dynClient, c.obj, c.managerApp)
			glog.Infof("Gitops pipeline detected.")
		default:
			return fmt.Errorf("unsupported gitops manager type: %v", c.managerApp.Type)
		}
		err := manager.Update(int64(*updatedSpec.replicas), podSpecUnstructured)
		if err != nil {
			return fmt.Errorf("failed to update the gitops managed source of truth: %v", err)
		}
		return nil
	}

	controllerOwnerReferences := c.obj.GetOwnerReferences()
	if !c.shouldSkipOperator(c.obj) && c.shouldUseOrm(c.obj) {
		// If k8s controller is controlled by custom controller, update the CR using OperatorResourceMapping
		// if SkipOperatorLabel is not set or not true.
		if c.ormClient == nil {
			return fmt.Errorf("failed to execute action with nil ORMClient")
		}
		err = c.ormClient.Update(origControllerObj, c.obj, controllerOwnerReferences[0])
	} else {
		_, err = c.clients.dynNamespacedClient.Update(context.TODO(), c.obj, metav1.UpdateOptions{})
	}
	return err
}

// Whether Operator controller should be skipped when executing a resize action on a K8s controller
// based on the label. If the SkipOperatorLabel is set to true on a K8s controller, resize action
// will directly update this controller regardless of upper Operator controller.
func (c *parentController) shouldSkipOperator(controller *unstructured.Unstructured) bool {
	labels := controller.GetLabels()
	if labels == nil {
		return false
	}
	labelVal, exists := labels[actionutil.SkipOperatorLabel]
	if exists && strings.EqualFold(labelVal, "true") {
		glog.Infof("Directly updating '%s %s/%s' regardless of Operator controller because '%s' label is set to true.",
			controller.GetKind(), controller.GetNamespace(), controller.GetName(), actionutil.SkipOperatorLabel)
		return true
	}
	return false
}

func (rc *parentController) String() string {
	return rc.name
}

//To determine if ORM should be used or not in terms of the owner reference of the work load controller
func (c *parentController) shouldUseOrm(controller *unstructured.Unstructured) bool {
	if controller == nil {
		glog.V(3).Infof("Workload controller pointer is nil, shouldUseOrm return false")
		return false
	}
	controllerOwnerReferences := c.obj.GetOwnerReferences()
	if len(controllerOwnerReferences) <= 0 {
		glog.V(3).Infof("Workload controller <%v/%v> doesn't have an owner reference, shouldUseOrm return false", controller.GetNamespace(), controller.GetName())
		return false
	}
	/*
		In the following 3 situations, we use trigger ORM to update the CR
	*/
	//Case 1: The owner of the controller has the kind as ClusterServiceVersion
	/*
		ownerReferences:
		- apiVersion: operators.coreos.com/v1alpha1
		  blockOwnerDeletion: false
		  controller: false
		  kind: ClusterServiceVersion
		  name: ibm-licensing-operator.v1.14.0
		  uid: bfd3d22d-99f9-473d-bea6-dd81b8d763ff
		resourceVersion: "438731373"
		uid: 6e58b1e7-dd29-41d5-8b77-194797b4fe9e
	*/
	if "ClusterServiceVersion" == controllerOwnerReferences[0].Kind {
		glog.V(2).Infof("Workload controller <%v/%v> has the owner with ClusterServiceVersion kind, shouldUseOrm return true", controller.GetNamespace(), controller.GetName())
		return true
	}

	//Case 2: The owner has the controller field and the value of the filed is set to True
	/*
	  ownerReferences:
	  - apiVersion: operator.ibm.com/v1alpha1
	    blockOwnerDeletion: true
	    controller: true
	    kind: IBMLicensing
	    name: instance
	    uid: 5882b63f-bd56-4d3b-83c7-b38405bf3429
	*/
	if controllerOwnerReferences[0].Controller != nil && *controllerOwnerReferences[0].Controller {
		glog.V(2).Infof("Workload controller <%v/%v> has the owner controller set to true, shouldUseOrm return true", controller.GetNamespace(), controller.GetName())
		return true
	}

	//Case 3: The owner doesn't even have the controller filed
	/*
		ownerReferences:
		- apiVersion: icp4a.ibm.com/v1
		  kind: ICP4ACluster
		  name: icp4adeploy
		  uid: d411a4bc-29bf-4086-9c5c-c65345256dcf
		resourceVersion: "228138709"
		uid: 21c125c3-4b5d-4037-8088-4b00c241e16e
	*/
	if controllerOwnerReferences[0].Controller == nil {
		glog.V(2).Infof("Workload controller <%v/%v> has the owner but the owner doesn't have controller field, shouldUseOrm return true", controller.GetNamespace(), controller.GetName())
		return true
	}

	return false
}
