package executor

import (
	"fmt"
	"github.com/golang/glog"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "k8s.io/client-go/kubernetes"
	k8sapi "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/util"
)

// update the Pod.Containers[index]'s Resources.Limits and Resources.Requests.
func updateCapacity(pod *k8sapi.Pod, index int, patchCapacity k8sapi.ResourceList) (bool, error) {
	glog.V(2).Infof("begin to update Capacity.")
	changed := false

	if index >= len(pod.Spec.Containers) {
		err := fmt.Errorf("Cannot find container[%d] in pod[%s]", index, pod.Name)
		glog.Error(err)
		return false, err
	}
	container := &(pod.Spec.Containers[index])

	//1. get the original capacities
	result := make(k8sapi.ResourceList)
	for k, v := range container.Resources.Limits {
		result[k] = v
	}

	//2. apply the patch
	for k, v := range patchCapacity {
		oldv, exist := result[k]
		if !exist || oldv.Cmp(v) != 0 {
			result[k] = v
			changed = true
		}
	}

	if !changed {
		return false, nil
	}
	container.Resources.Limits = result

	//3. adjust the requirements: if new capacity is less than requirement, reduce the requirement
	// TODO 1: discuss reduce the requirement, or increase the limit?
	// TODO 2: If Requests is omitted for a container, it defaults to Limits if that is explicitly specified,
	//      we have to set a value for the requests; how to decide the value?
	updateRequests(container, result)
	return changed, nil
}

func updateRequests(container *k8sapi.Container, limits k8sapi.ResourceList) error {
	zero := resource.NewQuantity(0, resource.BinarySI)
	glog.V(4).Infof("zero=%++v", zero)

	if container.Resources.Requests == nil {
		container.Resources.Requests = make(k8sapi.ResourceList)
	}
	requests := container.Resources.Requests

	for k, v := range limits {
		rv, exist := requests[k]
		if !exist {
			requests[k] = *zero
			continue
		}

		if rv.Cmp(v) > 0 {
			requests[k] = v
		}
	}

	return nil
}

// Generate a resource.Quantity for CPU.
// it will convert CPU unit from MHz to CPU.core time in milliSeconds
// @newValue is from OpsMgr, in MHz
// @cpuFrequency is from kubeletClient, in KHz
func genCPUQuantity(newValue float64, cpuFrequency uint64) (resource.Quantity, error) {
	tmp := uint64(newValue) * 1000 * 1000 //to KHz and to milliSeconds
	cpuTime := tmp / cpuFrequency
	if cpuTime < 1 {
		cpuTime = 1
	}

	result, err := resource.ParseQuantity(fmt.Sprintf("%dm", cpuTime))
	if err != nil {
		glog.Errorf("failed to generate CPU quantity: %v", err)
		return result, err
	}

	return result, nil
}

// generate a resource.Quantity for Memory
func genMemoryQuantity(newValue float64) (resource.Quantity, error) {
	tmp := int64(newValue)
	if tmp < 1 {
		tmp = 1
	}
	result, err := resource.ParseQuantity(fmt.Sprintf("%dKi", tmp))
	if err != nil {
		glog.Errorf("failed to generate Memory quantity: %v", err)
		return result, err
	}

	return result, nil
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
	if flag, err := updateCapacity(npod, index, capacity); err != nil {
		glog.Errorf("resizeContainer failed [%s]: failed to update container Capacity: %v", id, err)
		return err
	} else if !flag {
		glog.V(2).Infof("resizeContainer aborted [%s]: no need to resize.", id)
		return nil
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
	time.Sleep(time.Duration(grace)*time.Second + defaultMoreGrace)

	interval := defaultPodCreateSleep
	timeout := interval*time.Duration(retryNum) + time.Second*10
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
