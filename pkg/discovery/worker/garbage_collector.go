package worker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"

	api "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/action/executor"
	commonutil "github.com/turbonomic/kubeturbo/pkg/util"
)

var supportedGrandParents = []schema.GroupVersionResource{
	schema.GroupVersionResource{
		Group:    commonutil.K8sAPIDeploymentGV.Group,
		Version:  commonutil.K8sAPIDeploymentGV.Version,
		Resource: commonutil.DeploymentResName},
	schema.GroupVersionResource{
		Group:    commonutil.OpenShiftAPIDeploymentConfigGV.Group,
		Version:  commonutil.OpenShiftAPIDeploymentConfigGV.Version,
		Resource: commonutil.DeploymentConfigResName},
}

var supportedParents = []schema.GroupVersionResource{
	schema.GroupVersionResource{
		Group:    commonutil.K8sAPIReplicationControllerGV.Group,
		Version:  commonutil.K8sAPIReplicationControllerGV.Version,
		Resource: commonutil.ReplicationControllerResName},
	schema.GroupVersionResource{
		Group:    commonutil.K8sAPIReplicasetGV.Group,
		Version:  commonutil.K8sAPIReplicasetGV.Version,
		Resource: commonutil.ReplicaSetResName},
}

var gcListOpts = metav1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s", executor.TurboGCLabelKey, executor.TurboGCLabelVal)}

type GarbageCollector struct {
	client                *kubernetes.Clientset
	dynClient             dynamic.Interface
	finishCollecting      chan bool
	collectionIntervalSec int

	// Necessary for test
	podAge time.Duration
}

func NewGarbageCollector(client *kubernetes.Clientset, dynClient dynamic.Interface, finishChan chan bool, collectionIntervalSec int, podAge time.Duration) *GarbageCollector {
	return &GarbageCollector{
		podAge:                podAge,
		client:                client,
		dynClient:             dynClient,
		finishCollecting:      finishChan,
		collectionIntervalSec: collectionIntervalSec,
	}
}

func (g *GarbageCollector) StartCleanup() error {
	glog.V(4).Info("Start garbage cleanup.")

	// We cleanup the controllers before cleaning up the pods to ensure
	// that the pending (invalid controller) pods are deleted at the right time.
	if err := g.RevertControllers(); err != nil {
		return err
	}

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

	return nil
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
func (g *GarbageCollector) RevertControllers() error {
	if err := g.revertSchedulers(); err != nil {
		return err
	}

	if err := g.unpauseRollouts(); err != nil {
		return err
	}

	return nil
}

func (g *GarbageCollector) revertSchedulers() error {
	for _, parentRes := range supportedParents {
		objList, err := g.dynClient.Resource(parentRes).Namespace("").List(context.TODO(), gcListOpts)
		if err != nil {
			// we fail forcing kubeturbo to restart and try this again
			glog.Errorf("Error reverting scheduler of parent controller for %s: %v", parentRes.String(), err)
			return err
		}
		if objList == nil {
			// ignore
			return nil
		}
		for _, item := range objList.Items {
			valid := true
			err := commonutil.RetryDuring(executor.DefaultRetryLess, 0,
				executor.DefaultRetrySleepInterval, func() error {
					return executor.ChangeScheduler(g.dynClient.Resource(parentRes).Namespace(item.GetNamespace()), &item, valid)
				})
			if err != nil {
				glog.Errorf("Error reverting scheduler of parent controller for %s %s/%s: %v", item.GetKind(), item.GetNamespace(), item.GetName(), err)
				return err
			}
		}
	}

	return nil
}

func (g *GarbageCollector) unpauseRollouts() error {
	for _, grandParentRes := range supportedGrandParents {
		objList, err := g.dynClient.Resource(grandParentRes).Namespace("").List(context.TODO(), gcListOpts)
		if err != nil {
			// This error could be because of mising deploymentConfig type from the cluster, so we don't fail here
			if apierrors.IsNotFound(err) && strings.Contains(err.Error(), "the server could not find the requested resource") {
				glog.V(3).Infof("Resource not found enabled on cluster while resuming rollout of controllers %s: %v", grandParentRes.String(), err)
				continue
			}
			glog.Errorf("Error resuming rollout of controller %s: %v", grandParentRes.String(), err)
			return err
		}
		if objList == nil {
			// ignore
			return nil
		}
		for _, item := range objList.Items {
			unpause := false
			err := commonutil.RetryDuring(executor.DefaultRetryLess, 0,
				executor.DefaultRetrySleepInterval, func() error {
					return executor.ResourceRollout(g.dynClient.Resource(grandParentRes).Namespace(item.GetNamespace()), &item, unpause)
				})
			if err != nil {
				glog.Errorf("Error resuming rollout of controller %s %s/%s: %v", item.GetKind(), item.GetNamespace(), item.GetName(), err)
				return err
			}
		}

	}

	return nil
}
