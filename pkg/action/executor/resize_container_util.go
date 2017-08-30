package executor

import (
	"fmt"
	"time"
	"github.com/golang/glog"

	k8sapi "k8s.io/client-go/pkg/api/v1"
	kclient "k8s.io/client-go/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/turbonomic/kubeturbo/pkg/util"
)

// update the Pod.Containers[index]'s Resources.Limits and Resources.Requests.
func updateCapacity(pod *k8sapi.Pod, index int, patchCapacity k8sapi.ResourceList) error {
	if index >= len(pod.Spec.Containers) {
		err := fmt.Errorf("Cannot find container[%d] in pod[%s]", index, pod.Name)
		glog.Error(err)
		return err
	}
	container := &(pod.Spec.Containers[index])

	//1. get the original capacities
	result := make(k8sapi.ResourceList)
	for k, v := range container.Resources.Limits {
		result[k] = v
	}

	//2. apply the patch
	for k, v := range patchCapacity {
		result[k] = v
	}
	container.Resources.Limits = result

	//3. adjust the requirements: if new capacity is less than requirement, reduce the requirement
	// TODO: discuss reduce the requirement, or increase the limit?
	requests := container.Resources.Requests
	if requests == nil || len(requests) < 1 {
		return nil
	}

	for k, v := range result {
		rv, exist := requests[k]
		if exist && rv.Value() > v.Value() {
			requests[k] = v
		}
	}
	container.Resources.Requests = requests

	return nil
}

func resizeContainer(client *kclient.Clientset, pod *k8sapi.Pod, index int, capacity k8sapi.ResourceList, retryNum int) error {
	id := fmt.Sprintf("%s/%s-%d", pod.Namespace, pod.Name, index)
	glog.V(2).Infof("begin to resize Pod container[%s].", id)

	podClient := client.CoreV1().Pods(pod.Namespace)
	if podClient == nil {
		err := fmt.Errorf("resizeContainer failed [%s]: cannot get Pod client for nameSpace:%v", id, pod.Namespace)
		glog.Error(err)
		return err
	}

	// 1. copy the original pod
	npod := &k8sapi.Pod{}
	copyPodInfo(pod, npod)
	npod.Spec.NodeName = pod.Spec.NodeName

	//2. update resource capacity
	if err := updateCapacity(npod, index, capacity); err != nil {
		glog.Errorf("resizeContainer failed [%s]: failed to update container Capacity: %v", id, err)
		return err
	}

	//3. kill the original pod
	grace := calcGracePeriod(pod)
	delOption := &metav1.DeleteOptions{GracePeriodSeconds: &grace}
	if err := podClient.Delete(pod.Name, delOption); err != nil {
		err := fmt.Errorf("resizeContainer failed [%s]: failed to delete orginial pod: %v", id, err)
		glog.Error(err)
		return err
	}

	//4. create a new pod
	// wait for the previous pod to be cleaned up.
	time.Sleep(time.Duration(grace+defaultMoreGrace) * time.Second)

	interval := defaultPodCreateSleep
	timeout := interval * time.Duration(retryNum) + time.Second * 10
	err := util.RetryDuring(retryNum, timeout, interval, func() error {
		_, inerr := podClient.Create(npod)
		return inerr
	})
	if err != nil {
		err = fmt.Errorf("resizeContainer failed [%s]: failed to create new pod: %v", id, err)
		glog.Error(err)
		return err
	}

	glog.V(2).Infof("resizeContainer[%s] finished.", id)
	return nil
}
