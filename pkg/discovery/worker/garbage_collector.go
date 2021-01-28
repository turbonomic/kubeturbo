package worker

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/action/executor"
)

type GarbageCollector struct {
	client                *kubernetes.Clientset
	finishCollecting      chan bool
	collectionIntervalSec int

	// Necessary for test
	podAge time.Duration
}

func NewGarbageCollector(client *kubernetes.Clientset, finishChan chan bool, collectionIntervalSec int, podAge time.Duration) *GarbageCollector {
	return &GarbageCollector{
		podAge:                podAge,
		client:                client,
		finishCollecting:      finishChan,
		collectionIntervalSec: collectionIntervalSec,
	}
}

func (g *GarbageCollector) StartCleanup() {
	glog.V(4).Info("Start leaked pods cleanup.")
	// Also cleanup immediately at startup
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
}

func (g *GarbageCollector) cleanupLeakedClonePods() {
	// Get all pods which have the gc label. This can include those on which an
	// action is being executed right now.
	listOpts := metav1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s", executor.TurboGCLabelKey, executor.TurboGCLabelVal)}
	g.cleanupLeakedPods(listOpts)
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
