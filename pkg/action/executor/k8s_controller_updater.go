package executor

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/golang/glog"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kclient "k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/cluster"
	discoveryutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"github.com/turbonomic/kubeturbo/pkg/util"
)

// k8sControllerUpdater defines a specific k8s controller resource
// and the mechanism to obtain and update it
type k8sControllerUpdater struct {
	controller k8sController
	client     *kclient.Clientset
	name       string
	namespace  string
	podName    string
}

// controllerSpec defines the portion of a controller specification that we are interested in
// - replicasDiff: 1 for provision, -1 for suspend
// - resizeSpec: the index and new resource requirement of a container
type controllerSpec struct {
	replicasDiff int32
	resizeSpecs  []*containerResizeSpec
}

// newK8sControllerUpdaterViaPod returns a k8sControllerUpdater based on the parent kind of a pod
func newK8sControllerUpdaterViaPod(clusterScraper *cluster.ClusterScraper, pod *api.Pod, ormClient *resourcemapping.ORMClient) (*k8sControllerUpdater, error) {
	// Find parent kind of the pod
	ownerInfo, _, _, err := clusterScraper.GetPodControllerInfo(pod, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent info of pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}
	if discoveryutil.IsOwnerInfoEmpty(ownerInfo) {
		return nil, fmt.Errorf("pod %s/%s does not have controller", pod.Namespace, pod.Name)
	}
	return newK8sControllerUpdater(clusterScraper, ormClient, ownerInfo.Kind, ownerInfo.Name, pod.Name, pod.Namespace)
}

// newK8sControllerUpdater returns a k8sControllerUpdater based on the controller kind
func newK8sControllerUpdater(clusterScraper *cluster.ClusterScraper, ormClient *resourcemapping.ORMClient, kind, controllerName, podName, namespace string) (*k8sControllerUpdater, error) {
	res, err := GetSupportedResUsingKind(kind, namespace, controllerName)
	if err != nil {
		return nil, err
	}
	return &k8sControllerUpdater{
		controller: &parentController{
			dynClient:           clusterScraper.DynamicClient,
			dynNamespacedClient: clusterScraper.DynamicClient.Resource(res).Namespace(namespace),
			name:                kind,
			ormClient:           ormClient,
		},
		client:    clusterScraper.Clientset,
		name:      controllerName,
		namespace: namespace,
		podName:   podName,
	}, nil
}

func GetSupportedResUsingKind(kind, namespace, name string) (schema.GroupVersionResource, error) {
	res := schema.GroupVersionResource{}
	var err error
	switch kind {
	case util.KindReplicationController:
		res = schema.GroupVersionResource{
			Group:    util.K8sAPIReplicationControllerGV.Group,
			Version:  util.K8sAPIReplicationControllerGV.Version,
			Resource: util.ReplicationControllerResName}
	case util.KindReplicaSet:
		res = schema.GroupVersionResource{
			Group:    util.K8sAPIReplicasetGV.Group,
			Version:  util.K8sAPIReplicasetGV.Version,
			Resource: util.ReplicaSetResName}
	case util.KindDeployment:
		res = schema.GroupVersionResource{
			Group:    util.K8sAPIDeploymentGV.Group,
			Version:  util.K8sAPIDeploymentGV.Version,
			Resource: util.DeploymentResName}
	case util.KindDeploymentConfig:
		res = schema.GroupVersionResource{
			Group:    util.OpenShiftAPIDeploymentConfigGV.Group,
			Version:  util.OpenShiftAPIDeploymentConfigGV.Version,
			Resource: util.DeploymentConfigResName}
	case util.KindDaemonSet:
		res = schema.GroupVersionResource{
			Group:    util.K8sAPIDaemonsetGV.Group,
			Version:  util.K8sAPIDaemonsetGV.Version,
			Resource: util.DaemonSetResName}
	case util.KindStatefulSet:
		res = schema.GroupVersionResource{
			Group:    util.K8sAPIStatefulsetGV.Group,
			Version:  util.K8sAPIStatefulsetGV.Version,
			Resource: util.StatefulSetResName}
	default:
		err = fmt.Errorf("unsupport controller type %s for %s/%s", kind, namespace, name)
	}
	return res, err
}

// updateWithRetry updates a specific k8s controller with retry and timeout
func (c *k8sControllerUpdater) updateWithRetry(ctlrSpec *controllerSpec) error {
	retryNum := DefaultExecutionRetry
	interval := defaultUpdateReplicaSleep
	timeout := time.Duration(retryNum+1) * interval
	err := util.RetryDuring(retryNum, timeout, interval, func() error {
		return c.update(ctlrSpec)
	})
	return err
}

// update updates a specific k8s controller in the following steps:
// - Get and save the current controller object from the server
// - Reconcile the current specification with the desired specification, and
//   update the current specification in place if the changes are valid
// - Update the controller object with the server after a successful reconciliation
func (c *k8sControllerUpdater) update(desired *controllerSpec) error {
	glog.V(4).Infof("Begin to update %v %s/%s",
		c.controller, c.namespace, c.name)
	current, err := c.controller.get(c.name)
	if err != nil {
		return err
	}
	updated, err := c.reconcile(current, desired)
	if err != nil {
		return err
	}
	if !updated {
		glog.V(2).Infof("%v %s/%s has already been updated to the desired specification",
			c.controller, c.namespace, c.name)
		return nil
	}
	if err := c.controller.update(current); err != nil {
		return err
	}
	glog.V(2).Infof("Successfully updated %v %s/%s",
		c.controller, c.namespace, c.name)
	return nil
}

// reconcile validates the desired specification and updates the saved controller object in place
// Nothing is updated if there is no change needed. This can happen when resize actions
// from multiple pods that belong to the same controller are generated
func (c *k8sControllerUpdater) reconcile(current *k8sControllerSpec, desired *controllerSpec) (bool, error) {
	if desired.replicasDiff != 0 {
		// This is a horizontal scale
		// We want to suspend the target pod or provision a new pod first
		num, err := c.suspendOrProvision(*current.replicas, desired.replicasDiff)
		if err != nil {
			return false, err
		}
		// Update the replicas of the controller
		glog.V(2).Infof("Try to update replicas of %v %s/%s from %d to %d",
			c.controller, c.namespace, c.name, *current.replicas, num)
		*current.replicas = num
		return true, nil
	}
	// This may be a vertical scale
	// Check and update resource limits/requests of the container in the pod specification
	indexes := ""
	for _, spec := range desired.resizeSpecs {
		if indexes == "" {
			indexes = strconv.Itoa(spec.Index) + ","
		} else {
			indexes = indexes + strconv.Itoa(spec.Index) + ","
		}
	}
	glog.V(4).Infof("Try to update resources for container indexes: %s in pod template spec of %v %s/%s .",
		indexes, c.controller, c.namespace, c.name)
	updated, err := updateResourceAmount(current.podSpec, desired.resizeSpecs, current.controllerName)
	if err != nil {
		return false, err
	}
	return updated, nil
}

// suspendOrProvision suspends or provisions the target pod and
// returns the desired replica number after suspension or provision
// Note: For now we only suspend the target pod
// TODO: Provision a new pod on the target host
func (c *k8sControllerUpdater) suspendOrProvision(current, diff int32) (int32, error) {
	//1. validate replica number
	result := current + diff
	glog.V(4).Infof("Current replica %d, diff %d, result %d.", current, diff, result)
	if result < 1 {
		return 0, fmt.Errorf("resulting replica is less than 1 after suspension")
	}
	//2. suspend the target
	if diff < 0 {
		if err := c.suspendPod(); err != nil {
			return 0, err
		}
	}
	return result, nil
}

// suspendPod takes the name of a pod and deletes it
// The call does not block
func (c *k8sControllerUpdater) suspendPod() error {
	podClient := c.client.CoreV1().Pods(c.namespace)
	if _, err := podClient.Get(context.TODO(), c.podName, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("failed to get latest pod %s/%s: %v", c.namespace, c.podName, err)
	}
	// This function does not block
	if err := podClient.Delete(context.TODO(), c.podName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete pod %s/%s: %v", c.namespace, c.podName, err)
	}
	glog.V(2).Infof("Successfully suspended pod %s/%s", c.namespace, c.podName)
	return nil
}
