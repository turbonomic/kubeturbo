package worker

import (
	"context"
	"fmt"
	"github.ibm.com/turbonomic/kubeturbo/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"time"

	"github.com/golang/glog"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.ibm.com/turbonomic/kubeturbo/pkg/action/executor"
	commonutil "github.ibm.com/turbonomic/kubeturbo/pkg/util"
)

var supportedGrandParentK8s = schema.GroupVersionResource{
	Group:    commonutil.K8sAPIDeploymentGV.Group,
	Version:  commonutil.K8sAPIDeploymentGV.Version,
	Resource: commonutil.DeploymentResName,
}

var supportedGrandParentOcp = schema.GroupVersionResource{
	Group:    commonutil.OpenShiftAPIDeploymentConfigGV.Group,
	Version:  commonutil.OpenShiftAPIDeploymentConfigGV.Version,
	Resource: commonutil.DeploymentConfigResName,
}

// Grandparents for non-OpenShift clusters
var supportedGrandParentsK8s = []schema.GroupVersionResource{
	supportedGrandParentK8s,
}

// Grandparents for OpenShift clusters
var supportedGrandParentsOcp = []schema.GroupVersionResource{
	supportedGrandParentK8s,
	supportedGrandParentOcp,
}

var supportedParents = []schema.GroupVersionResource{
	{
		Group:    commonutil.K8sAPIReplicationControllerGV.Group,
		Version:  commonutil.K8sAPIReplicationControllerGV.Version,
		Resource: commonutil.ReplicationControllerResName,
	},
	{
		Group:    commonutil.K8sAPIReplicasetGV.Group,
		Version:  commonutil.K8sAPIReplicasetGV.Version,
		Resource: commonutil.ReplicaSetResName,
	},
}

var gcListOpts = metav1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s", executor.TurboGCLabelKey, executor.TurboGCLabelVal)}

type GarbageCollector struct {
	client                *kubernetes.Clientset
	dynClient             dynamic.Interface
	finishCollecting      chan bool
	collectionIntervalSec int
	supportedGrandParents []schema.GroupVersionResource

	// Necessary for test
	podAge time.Duration
}

func NewGarbageCollector(client *kubernetes.Clientset, dynClient dynamic.Interface, finishChan chan bool, collectionIntervalSec int, podAge time.Duration, isOpenShift bool) *GarbageCollector {
	return &GarbageCollector{
		podAge:                podAge,
		client:                client,
		dynClient:             dynClient,
		finishCollecting:      finishChan,
		collectionIntervalSec: collectionIntervalSec,
		supportedGrandParents: getSupportedGrandParents(isOpenShift),
	}
}

func getSupportedGrandParents(isOpenShift bool) []schema.GroupVersionResource {
	if isOpenShift {
		return supportedGrandParentsOcp
	} else {
		return supportedGrandParentsK8s
	}
}

// The cleanup routine does not error out. It rather retries couple of times on an api error
// and continues ahead with trying to revert/clean up other items leaving the failure case behind.
// The opportunity to revert/cleanup would arise only if and when kubeturbo restarts again.
// This is a stop gap solution for resources which can possibly be left behind as a result of
// kubeturbo restarts while resources were in interim states wrt a kubeturbo action(s).
// Additionally we use "defer" for all the cleanup/revert of resources updated as part of a
// kubeturbo action which most certainly will be executed even if kubeturbo panics, however
// the api calls are not guaranteed to suceed in defer, giving a small chance that some resource
// might still be left in a bad state. The cleanup/revert routines here are sort of an additional
// safety net to put such resources back in a good state. This also therefor does not guarantee
// making the resources healthy and/or a proper cleanup.
// An absolutely error free solution will be implementing kubeturbo action executor to support
// eventually consistent actions.
// For possible leaked pods however we continue to try at 10 min intervals, while kubeturbo goes
// ahead with rest of its tasks in parallel.
func (g *GarbageCollector) StartCleanup() {
	glog.V(4).Info("Start garbage cleanup.")

	// We cleanup the controllers before cleaning up the pods to ensure
	// that the pending (invalid controller) pods are deleted at the right time.
	g.revertControllers()

	go func() {
		// Also cleanup immediately at startup
		// The pods cleanup is still done in a goroutine to ensure that kubeturbo does
		// not block long for pods being cleaned up.
		// TODO: As an improvement exit the goroutine after determining that all
		// leaked pods have been cleaned up successfully in this run.
		g.cleanupLeakedClonePods()
		g.cleanupLeakedWrongSchedulerPods()
		go func() {
			collectionInterval := time.Duration(g.collectionIntervalSec) * time.Second
			ticker := time.NewTicker(collectionInterval)
			defer ticker.Stop()
			for {
				select {
				case <-g.finishCollecting:
					// This would happen when kubeturbo is exiting
					return
				case <-ticker.C:
					g.cleanupLeakedClonePods()
					g.cleanupLeakedWrongSchedulerPods()
				}
			}
		}()
		<-g.finishCollecting
	}()

	// The quota cleanup can happen in parallel to the pods cleanup
	g.cleanupQuotas()
}

func (g *GarbageCollector) cleanupLeakedClonePods() {
	// Get all pods which have the gc label. This can include those on which an
	// action is being executed right now.
	g.cleanupLeakedPods(gcListOpts)
}

func (g *GarbageCollector) cleanupLeakedWrongSchedulerPods() {
	// Get those pods which are created when we set the parents (eg replicaset)
	// scheduler to dummy ("turbo-scheduler") for move actions for pods with volumes.
	listOpts := metav1.ListOptions{FieldSelector: fmt.Sprintf("spec.schedulerName=%s", executor.DummyScheduler)}
	g.cleanupLeakedPods(listOpts)
}

func (g *GarbageCollector) cleanupLeakedPods(listOpts metav1.ListOptions) {
	podList, err := g.client.CoreV1().Pods("").List(context.TODO(), listOpts)
	if err != nil {
		glog.Warningf("Error getting leaked pods: %v", err)
		return
	}
	if podList == nil {
		// Nothing to clean
		return
	}

	for _, pod := range podList.Items {
		if g.isLeakedPod(pod) {
			err := g.client.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			if err != nil {
				glog.Warningf("Encountered error trying to clean up leaked pod: %s/%s: %v", pod.Name, pod.Namespace, err)
			}
		}
	}
}

func (g *GarbageCollector) isLeakedPod(pod api.Pod) bool {
	labels := pod.Labels
	creationTime := pod.CreationTimestamp
	if labels != nil {
		gcLabelVal, gcLabelExists := labels[executor.TurboGCLabelKey]
		// This assumes that the cluster is running a synced os time.
		// In a remote case of a misconfigured cluster, there is a possibility of
		// a move actions failing because of this. But this cluster misconfiguration also
		// means that any other controller can also misbehave.
		if gcLabelExists && gcLabelVal == executor.TurboGCLabelVal && creationTime.Add(g.podAge).Before(time.Now()) {
			// No action would persist the cloned pod for 30 mins without updating it to correct set of labels.
			return true
		}
	}

	if pod.Spec.SchedulerName == executor.DummyScheduler && creationTime.Add(g.podAge).Before(time.Now()) {
		return true
	}

	return false
}

// The controllers cleanup is supposed to happen only at the startup
// We fail in case of errors to force kubeturbo to restart and try the cleanup again
func (g *GarbageCollector) revertControllers() {
	if utilfeature.DefaultFeatureGate.Enabled(features.VirtualMachinePodMove) {
		g.cleanupVmMoveAffinity()
	}
	g.revertSchedulers()
	g.unpauseRollouts()
}

// cleanupVmAffinity removes the affinity left behind by Kubeturbo when it crashes in the middle of moving a VM.
func (g *GarbageCollector) cleanupVmMoveAffinity() {
	var listOpts = metav1.ListOptions{LabelSelector: executor.TurboMoveLabelKey}
	var objList *unstructured.UnstructuredList
	err := commonutil.RetryDuring(executor.DefaultExecutionRetry, 0,
		executor.DefaultRetrySleepInterval, func() error {
			var internalErr error
			objList, internalErr = g.dynClient.Resource(commonutil.OpenShiftVirtualMachineGVR).Namespace("").List(context.TODO(), listOpts)
			return internalErr
		})
	if err != nil {
		glog.Errorf("Error cleaning up VM move affinity: %v.", err)
		return
	}
	for _, item := range objList.Items {
		labels := item.GetLabels()
		if labels == nil {
			// this shouldn't happen as we just got the list with label - log an error
			glog.Errorf("Error retrieving labels from %s %s/%s: %v", item.GetKind(), item.GetNamespace(), item.GetName(), err)
			continue
		}
		nodeName := labels[executor.TurboMoveLabelKey]
		err := executor.RemoveNodeAffinity(g.dynClient.Resource(commonutil.OpenShiftVirtualMachineGVR).Namespace(item.GetNamespace()), &item, nodeName)
		if err != nil {
			glog.Errorf("Error cleaning up move affinity for %s %s/%s: %v", item.GetKind(), item.GetNamespace(), item.GetName(), err)
		}
	}
}

func (g *GarbageCollector) revertSchedulers() {
	for _, parentRes := range supportedParents {
		var objList *unstructured.UnstructuredList
		err := commonutil.RetryDuring(executor.DefaultExecutionRetry, 0,
			executor.DefaultRetrySleepInterval, func() error {
				var internalErr error
				objList, internalErr = g.dynClient.Resource(parentRes).Namespace("").List(context.TODO(), gcListOpts)
				return internalErr
			})
		if err != nil {
			// We don't force kubeturbo to restart. Further retries to this are possible only at kubeturbo restarts.
			glog.Errorf("Error reverting scheduler of parent controller for %s: %v.", parentRes.String(), err)
		}
		if objList == nil {
			// ignore and try other items
			continue
		}
		for _, item := range objList.Items {
			valid := true
			err := commonutil.RetryDuring(executor.DefaultExecutionRetry, 0,
				executor.DefaultRetrySleepInterval, func() error {
					return executor.ChangeScheduler(g.dynClient.Resource(parentRes).Namespace(item.GetNamespace()), &item, valid)
				})
			if err != nil {
				// we tried couple of times but still saw failures, retrying would be possible only on kubeturbo restarts
				glog.Errorf("Error reverting scheduler of parent controller for %s %s/%s: %v", item.GetKind(), item.GetNamespace(), item.GetName(), err)
			}
		}
	}
}

func (g *GarbageCollector) unpauseRollouts() {
	for _, grandParentRes := range g.supportedGrandParents {
		var objList *unstructured.UnstructuredList
		err := commonutil.RetryDuring(executor.DefaultExecutionRetry, 0,
			executor.DefaultRetrySleepInterval, func() error {
				var internalErr error
				objList, internalErr = g.dynClient.Resource(grandParentRes).Namespace("").List(context.TODO(), gcListOpts)
				return internalErr
			})
		if err != nil {
			// We don't force kubeturbo to restart. Further retries to this are possible only at kubeturbo restarts.
			glog.Errorf("Error resuming rollout of controller %s: %v", grandParentRes.String(), err)
		}
		if objList == nil {
			// ignore and try other items
			continue
		}
		for _, item := range objList.Items {
			unpause := false
			err := commonutil.RetryDuring(executor.DefaultExecutionRetry, 0,
				executor.DefaultRetrySleepInterval, func() error {
					return executor.ResourceRollout(g.dynClient.Resource(grandParentRes).Namespace(item.GetNamespace()), &item, unpause)
				})
			if err != nil {
				glog.Errorf("Error resuming rollout of controller %s %s/%s: %v", item.GetKind(), item.GetNamespace(), item.GetName(), err)
			}
		}
	}
}

func (g *GarbageCollector) cleanupQuotas() {
	var quotaList *api.ResourceQuotaList
	err := commonutil.RetryDuring(executor.DefaultExecutionRetry, 0,
		executor.DefaultRetrySleepInterval, func() error {
			var internalErr error
			quotaList, internalErr = g.client.CoreV1().ResourceQuotas("").List(context.TODO(), gcListOpts)
			return internalErr
		})
	if err != nil {
		glog.Errorf("Error garbage cleaning quotas: %v", err)
	}
	if quotaList == nil {
		// ignore
		return
	}

	for _, quota := range quotaList.Items {
		revertQuota(g.client, &quota)
	}
}

func revertQuota(client *kubernetes.Clientset, quota *api.ResourceQuota) {
	var revertedQuota *api.ResourceQuota
	var err error
	if quota.Annotations != nil {
		origSpec, exists := quota.Annotations[commonutil.QuotaAnnotationKey]
		if exists {
			revertedQuota, err = commonutil.DecodeQuota([]byte(origSpec))
			if err != nil {
				// This is very unlikely but not an error which we can recover from, for example in the next run.
				// We need to ignore this. The annotation if there still is any would then carry the opportunity
				// for an user to use this as a reference in case a manual correction is required ever.
				glog.Warningf("Error reverting quota while garbage collecting resources: %s/%s: %v", quota.Namespace, quota.Name, err)
			}
		}
	}
	if revertedQuota != nil {
		// Although unlikely, revertedQuota being nil possibly is the result of the decode error above
		// so we go ahead and try to remove the GC label alone and leave the annotation behind.
		quota.Spec.Hard = revertedQuota.Spec.Hard
		RemoveTurboAnnotionFromQuota(quota)
	}
	executor.RemoveGCLabelFromQuota(quota)

	err = commonutil.RetryDuring(executor.DefaultExecutionRetry, 0,
		executor.DefaultRetrySleepInterval, func() error {
			_, err := client.CoreV1().ResourceQuotas(quota.Namespace).Update(context.TODO(), quota, metav1.UpdateOptions{})
			return err
		})
	if err != nil {
		glog.Errorf("Error reverting quota while garbage collecting resources: %s/%s: %v", quota.Namespace, quota.Name, err)
	}
}

func RemoveTurboAnnotionFromQuota(quota *api.ResourceQuota) {
	annotations := quota.GetAnnotations()
	if annotations == nil {
		// nothing to do
		return
	}
	if _, exists := annotations[commonutil.QuotaAnnotationKey]; exists {
		delete(annotations, commonutil.QuotaAnnotationKey)
		quota.SetAnnotations(annotations)
	}
}
