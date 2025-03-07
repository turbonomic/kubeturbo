package executor

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kclient "k8s.io/client-go/kubernetes"

	"github.ibm.com/turbonomic/kubeturbo/pkg/action/executor/gitops"
	actionutil "github.ibm.com/turbonomic/kubeturbo/pkg/action/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	discoveryutil "github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
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
// - desiredReplicas: desired number of replicas, preferred over the diff below
// - replicasDiff: 1 for provision, -1 for suspend
// - resizeSpec: the index and new resource requirement of a container
type controllerSpec struct {
	desiredReplicas int32
	replicasDiff    int32
	resizeSpecs     []*containerResizeSpec
}

// newK8sControllerUpdaterViaPod returns a k8sControllerUpdater based on the parent kind of a pod
func newK8sControllerUpdaterViaPod(clusterScraper *cluster.ClusterScraper, pod *api.Pod,
	ormClient *resourcemapping.ORMClientManager, gitConfig gitops.GitConfig, clusterId string,
	actionType proto.ActionItemDTO_ActionType) (*k8sControllerUpdater, error) {
	// Find parent kind of the pod
	ownerInfo, _, _, err := clusterScraper.GetPodControllerInfo(pod, true)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent info of pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}
	if discoveryutil.IsOwnerInfoEmpty(ownerInfo) {
		return nil, fmt.Errorf("pod %s/%s does not have controller", pod.Namespace, pod.Name)
	}
	// TODO: For an action on a parent workload controller of this pod managed by gitops (argoCD) we will need
	// some information to be available on the entity we get in the action (pod here).
	// We currently put the info about the argoCD manager app on the workload controller it manages.
	// Copy that data on to the pod also to ensure we can rightly identify if this pods parent is managed
	// by a specific argoCD app. (or find a better way of doing this)
	// As of now scale up and down actions wont work with argoCD.
	return newK8sControllerUpdater(clusterScraper, ormClient, ownerInfo.Kind, ownerInfo.Name,
		pod.Name, pod.Namespace, clusterId, nil, gitConfig, actionType)
}

// newK8sControllerUpdater returns a k8sControllerUpdater based on the controller kind
func newK8sControllerUpdater(clusterScraper *cluster.ClusterScraper,
	ormClient *resourcemapping.ORMClientManager, kind, controllerName, podName, namespace, clusterId string,
	managerApp *repository.K8sApp, gitConfig gitops.GitConfig,
	actionType proto.ActionItemDTO_ActionType) (*k8sControllerUpdater, error) {
	res, err := GetSupportedResUsingKind(kind, namespace, controllerName)
	if err != nil {
		return nil, err
	}

	return &k8sControllerUpdater{
		controller: &parentController{
			clients: kubeClients{
				typedClient:         clusterScraper.Clientset,
				osClient:            clusterScraper.OsClient,
				dynClient:           clusterScraper.DynamicClient,
				dynNamespacedClient: clusterScraper.DynamicClient.Resource(res).Namespace(namespace),
			},
			name:                  kind,
			ormClient:             ormClient,
			managerApp:            managerApp,
			gitConfig:             gitConfig,
			k8sClusterId:          clusterId,
			gitOpsConfigCache:     clusterScraper.GitOpsConfigCache,
			gitOpsConfigCacheLock: &clusterScraper.GitOpsConfigCacheLock,
			actionType:            actionType,
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
	case util.KindVirtualMachine:
		res = util.OpenShiftVirtualMachineGVR

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
//   - Get and save the current controller object from the server
//   - Reconcile the current specification with the desired specification, and
//     update the current specification in place if the changes are valid
//   - Update the controller object with the server after a successful reconciliation
func (c *k8sControllerUpdater) update(desired *controllerSpec) error {
	glog.V(2).Infof("Begin to update %v %s/%s ...",
		c.controller, c.namespace, c.name)
	current, err := c.controller.get(c.name)
	if err != nil {
		return err
	}
	updated, err := c.reconcile(current, desired)
	if err != nil {
		return err
	}
	if !updated && c.podName != util.ComputeContainerName {
		glog.V(2).Infof("%v %s/%s has already been updated to the desired specification.",
			c.controller, c.namespace, c.name)
		return nil
	}
	if err := c.controller.update(current); err != nil {
		return err
	}
	glog.V(2).Infof("Successfully updated %v %s/%s.",
		c.controller, c.namespace, c.name)
	return nil
}

// reconcile validates the desired specification and updates the saved controller object in place
// Nothing is updated if there is no change needed. This can happen when resize actions
// from multiple pods that belong to the same controller are generated
func (c *k8sControllerUpdater) reconcile(current *k8sControllerSpec, desired *controllerSpec) (bool, error) {
	limitRanges, err := c.client.CoreV1().LimitRanges(c.namespace).List(context.TODO(), metav1.ListOptions{})
	if err == nil && len(limitRanges.Items) > 0 {
		actionutil.SanitizeResources(current.podSpec)
	}
	// The desiredReplicas is recommended by the market based on the analysis of SLO metrics and existing RUNNING
	// replicas. We would like the workload to scale to this number, regardless whether there are non-Running
	// replicas or not. This makes the horizontal scale actions idempotent from the perspective of the market.
	if desired.desiredReplicas > 0 {
		// TODO: We should support scaling down to 0 in the future
		// This is a horizontal scale;
		// Update the replicas of the controller
		glog.V(2).Infof("Try to update replicas of %v %s/%s from %d to %d.",
			c.controller, c.namespace, c.name, *current.replicas, desired.desiredReplicas)
		*current.replicas = desired.desiredReplicas
		return true, nil
	}
	// The horizontal scale of current replicas by replicasDiff is deprecated, because this makes the horizontal scale
	// actions non-idempotent.
	// For example, in market analysis 1, there is a scale up action from 3 to 5 (a diff of 2), and it takes very long
	// for the new replicas to start up, so the new replicas are not picked up by the subsequent discovery. Then in
	// market analysis 2, the market still sees 3 running replicas, and continues recommending scaling up from 3 to 5,
	// resulting in 3 + 2 + 2 = 7 replicas.
	if desired.replicasDiff != 0 {
		// This is a horizontal scale
		// We want to suspend the target pod or provision a new pod first
		num, err := c.suspendOrProvision(*current.replicas, desired.replicasDiff)
		if err != nil {
			return false, err
		}
		// Update the replicas of the controller
		glog.V(2).Infof("Try to update replicas of %v %s/%s from %d to %d.",
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
	glog.V(2).Infof("Try to update resources for container indexes: %s in pod template spec of %v %s/%s .",
		indexes, c.controller, c.namespace, c.name)
	updated, err := updateResourceAmount(current.podSpec, desired.resizeSpecs, fmt.Sprintf("%s/%s", c.namespace, c.name))
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
	namespace := c.namespace
	podName := c.podName
	// Check if namespace is empty
	if namespace == "" {
		glog.Warningf("namespace value is not specified")
		return nil
	}

	// Check if pod name is empty or nil
	if podName == "" {
		glog.Warningf("pod name is not specified")
		return nil
	}
	podClient := c.client.CoreV1().Pods(namespace)
	if _, err := podClient.Get(context.TODO(), podName, metav1.GetOptions{}); err != nil {
		return fmt.Errorf("failed to get latest pod %s/%s: %v", namespace, podName, err)
	}
	// This function does not block
	if err := podClient.Delete(context.TODO(), c.podName, metav1.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete pod %s/%s: %v", namespace, podName, err)
	}
	glog.V(2).Infof("Successfully suspended pod %s/%s", namespace, podName)
	return nil
}

// revert the change
func (c *k8sControllerUpdater) revertChange() error {
	return c.controller.revert()
}
