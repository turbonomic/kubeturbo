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
	collectionIntervalMin int
}

func NewGarbageCollector(client *kubernetes.Clientset, finishChan chan bool, collectionIntervalMin int) *GarbageCollector {
	return &GarbageCollector{
		client:                client,
		finishCollecting:      finishChan,
		collectionIntervalMin: collectionIntervalMin,
	}
}

func (g *GarbageCollector) StartCleanup() {
	glog.V(4).Info("Start leaked pods cleanup.")
	// Also cleanup at startup
	g.cleanupLeakedPods()
	go func() {
		collectionInterval := time.Duration(g.collectionIntervalMin) * time.Minute
		// Create a ticker to schedule dispatch based on given sampling interval
		ticker := time.NewTicker(collectionInterval)
		defer ticker.Stop()
		for {
			select {
			case <-g.finishCollecting:
				// This would happen when kubeturbo is exiting
				return
			case <-ticker.C:
				g.cleanupLeakedPods()
			}
		}
	}()
}

func (d *GarbageCollector) cleanupLeakedPods() {
	// get all pods which have the gc annotation and are more then 30 mins old
	listOpts := metav1.ListOptions{LabelSelector: fmt.Sprintf("%s=%s", executor.TurboGCLabelKey, executor.TurboGCLabelVal)}
	podList, err := d.client.CoreV1().Pods("").List(context.TODO(), listOpts)
	if err != nil {
		glog.Warningf("Error getting leaked pods: %v", err)
	}
	if podList == nil {
		// Nothing to clean
		return
	}

	for _, pod := range podList.Items {
		if isLeakedPod(pod) {
			err := d.client.CoreV1().Pods(pod.Namespace).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			if err != nil {
				glog.Warningf("Encountered error trying to clean up leaked pods: %v", err)
			}
		}
	}
}

func isLeakedPod(pod api.Pod) bool {
	labels := pod.Labels
	creationTime := pod.CreationTimestamp
	if labels != nil {
		gcLabelVal, gcLabelExists := labels[executor.TurboGCLabelKey]
		// This assumes that the cluster is running a synced os time.
		if gcLabelExists && gcLabelVal == executor.TurboGCLabelVal && creationTime.Add(time.Minute*30).Before(time.Now()) {
			// No action would persist the cloned pod for 30 mins without updating it to correct set of labels.
			return true
		}
	}

	return false
}
