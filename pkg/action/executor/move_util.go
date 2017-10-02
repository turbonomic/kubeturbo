package executor

import (
	"fmt"
	"github.com/golang/glog"
	"time"

	"github.com/turbonomic/kubeturbo/pkg/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"
)

//TODO: check which fields should be copied
func copyPodInfo(oldPod, newPod *api.Pod) {
	//1. typeMeta
	newPod.TypeMeta = oldPod.TypeMeta

	//2. objectMeta
	newPod.ObjectMeta = oldPod.ObjectMeta
	newPod.SelfLink = ""
	newPod.ResourceVersion = ""
	newPod.Generation = 0
	newPod.CreationTimestamp = metav1.Time{}
	newPod.DeletionTimestamp = nil
	newPod.DeletionGracePeriodSeconds = nil

	//3. podSpec
	spec := oldPod.Spec
	spec.Hostname = ""
	spec.Subdomain = ""
	spec.NodeName = ""

	newPod.Spec = spec
	return
}

func calcGracePeriod(pod *api.Pod) int64 {
	grace := podDeletionGracePeriodDefault
	if pod.Spec.TerminationGracePeriodSeconds != nil {
		grace = *(pod.Spec.TerminationGracePeriodSeconds)
		if grace > podDeletionGracePeriodMax {
			grace = podDeletionGracePeriodMax
		}
	}
	return grace
}

// move pod nameSpace/podName to node nodeName
func movePod(client *kclient.Clientset, pod *api.Pod, nodeName string, retryNum int) (*api.Pod, error) {
	podClient := client.CoreV1().Pods(pod.Namespace)
	if podClient == nil {
		err := fmt.Errorf("cannot get Pod client for nameSpace:%v", pod.Namespace)
		glog.Error(err)
		return nil, err
	}

	//1. copy the original pod
	id := fmt.Sprintf("%v/%v", pod.Namespace, pod.Name)
	glog.V(2).Infof("move-pod: begin to move %v from %v to %v",
		id, pod.Spec.NodeName, nodeName)

	npod := &api.Pod{}
	copyPodInfo(pod, npod)
	npod.Spec.NodeName = nodeName

	//2. kill original pod
	grace := calcGracePeriod(pod)
	delOption := &metav1.DeleteOptions{GracePeriodSeconds: &grace}
	err := podClient.Delete(pod.Name, delOption)
	if err != nil {
		err = fmt.Errorf("move-failed: failed to delete original pod-%v: %v",
			id, err)
		glog.Error(err)
		return nil, err
	}

	//3. create (and bind) the new Pod
	time.Sleep(time.Duration(grace)*time.Second + defaultMoreGrace) //wait for the previous pod to be cleaned up.
	interval := defaultPodCreateSleep
	timeout := time.Duration(retryNum+1) * interval
	err = util.RetryDuring(retryNum, timeout, interval, func() error {
		_, inerr := podClient.Create(npod)
		return inerr
	})
	if err != nil {
		err = fmt.Errorf("move-failed: failed to create new pod-%v: %v",
			id, err)
		glog.Error(err)
		return nil, err
	}

	glog.V(2).Infof("move-finished: %v from %v to %v",
		id, pod.Spec.NodeName, nodeName)

	return npod, nil
}
