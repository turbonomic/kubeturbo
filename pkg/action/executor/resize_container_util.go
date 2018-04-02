package executor

import (
	"fmt"
	"github.com/golang/glog"
	"math"

	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "k8s.io/client-go/kubernetes"
	k8sapi "k8s.io/client-go/pkg/api/v1"
)

// update the Pod.Containers[index]'s Resources.Requests
func updateReservation(container *k8sapi.Container, patchCapacity k8sapi.ResourceList) (bool, error) {
	glog.V(4).Infof("begin to update Request(Reservation).")
	changed := false

	//1. get the original capacities
	result := make(k8sapi.ResourceList)
	for k, v := range container.Resources.Requests {
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
	container.Resources.Requests = result
	return true, nil
}

// update the Pod.Containers[index]'s Resources.Limits
func updateCapacity(container *k8sapi.Container, patchCapacity k8sapi.ResourceList) (bool, error) {
	glog.V(4).Infof("begin to update Capacity.")
	changed := false

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

	return true, nil
}

// make sure that the Request.Value is not bigger than the Limit.Value
// Note: It is certain that OpsMgr will make sure reservation is less than capacity.
func checkLimitsRequests(container *k8sapi.Container) error {
	if container.Resources.Limits == nil || container.Resources.Requests == nil {
		return nil
	}

	limits := container.Resources.Limits
	requests := container.Resources.Requests

	for k, v := range limits {
		rv, exist := requests[k]
		if !exist {
			continue
		}

		if rv.Cmp(v) > 0 {
			err := fmt.Errorf("Rquested resource is larger than limits: %v %v Vs. %v", k.String(), v.String(), rv.String())
			glog.Errorf(err.Error())
			return err
		}
	}

	return nil
}

func updateResourceAmount(pod *k8sapi.Pod, spec *containerResizeSpec) (bool, error) {
	index := spec.Index
	fullName := fmt.Sprintf("%s/%s-%d", pod.Namespace, pod.Name, index)
	glog.V(4).Infof("Begin to update container %v resources.", fullName)

	//1. get container
	if index >= len(pod.Spec.Containers) {
		err := fmt.Errorf("Cannot find container[%d] in pod[%s]", index, pod.Name)
		glog.Error(err)
		return false, err
	}
	container := &(pod.Spec.Containers[index])

	//2. update Limits
	flag := false
	if spec.NewCapacity != nil && len(spec.NewCapacity) > 0 {
		cflag, err := updateCapacity(container, spec.NewCapacity)
		if err != nil {
			glog.Errorf("Failed to set new Limits for container %v: %v", fullName, err)
			return false, err
		}
		glog.V(4).Infof("Set Limits for container %v successfully.", fullName)
		flag = flag || cflag
	}

	//3. update Requests
	if spec.NewRequest != nil && len(spec.NewRequest) > 0 {
		rflag, err := updateReservation(container, spec.NewRequest)
		if err != nil {
			glog.Errorf("Failed to set new Requests for container %v: %v", fullName, err)
			return false, err
		}

		glog.V(4).Infof("Set Requests for container %v successfully.", fullName)
		flag = flag || rflag
	}

	//4. check the new Limits vs. Requests, make sure Limits >= Requests
	if err := checkLimitsRequests(container); err != nil {
		glog.Errorf("Failed to check Limits Vs. Requests for container %v: %++v", fullName, spec)
		return false, err
	}

	return flag, nil
}

// Generate a resource.Quantity for CPU.
// it will convert CPU unit from MHz to CPU.core time in milliSeconds
// @newValue is from OpsMgr, in MHz
// @cpuFrequency is from kubeletClient, in KHz
func genCPUQuantity(newValue float64, cpuFrequency uint64) (resource.Quantity, error) {
	tmp := newValue * 1000 * 1000 //to KHz and to milliSeconds
	tmp = tmp / float64(cpuFrequency)
	cpuTime := int(math.Ceil(tmp))
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

// Resize pod in three steps:
//   step1: create a clone pod of the original pod (without labels), with new resource limits/requests;
//   step2: delete the orginal pod;
//   step3: add the labels to the cloned pod;
func resizeContainer(client *kclient.Clientset, tpod *k8sapi.Pod, spec *containerResizeSpec, retryNum int) (*k8sapi.Pod, error) {
	index := spec.Index
	id := fmt.Sprintf("%s/%s-%d", tpod.Namespace, tpod.Name, index)
	glog.V(2).Infof("begin to resize Pod container[%s].", id)

	podClient := client.CoreV1().Pods(tpod.Namespace)
	if podClient == nil {
		err := fmt.Errorf("cannot get Pod client for nameSpace:%v", tpod.Namespace)
		glog.Error(err)
		return nil, err
	}

	// 0. get the latest Pod
	var err error
	var pod *k8sapi.Pod
	if pod, err = podClient.Get(tpod.Name, metav1.GetOptions{}); err != nil {
		glog.Errorf("Failed to get latest pod %v: %v", id, err)
		return nil, err
	}
	labels := pod.Labels

	// 1. create a clone pod
	npod, changed, err := createResizePod(client, pod, spec)
	if err != nil {
		glog.Errorf("resizeContainer failed[%s]: failed to create a resized pod: %v", id, err)
		return nil, err
	}

	if !changed {
		glog.Warningf("resizeContainer aborted[%s]: no need do resize container.", id)
		return nil, fmt.Errorf("Aborted due to not enough change")
	}
	//delete the clone pod if this action fails
	flag := false
	defer func() {
		if !flag {
			glog.Errorf("Resize pod failed, begin to delete cloned pod: %v/%v", npod.Namespace, npod.Name)
			delOpt := &metav1.DeleteOptions{}
			podClient.Delete(npod.Name, delOpt)
		}
	}()

	//1.2 wait until podC gets ready
	err = waitForReady(client, npod.Namespace, npod.Name, "", retryNum)
	if err != nil {
		glog.Errorf("Wait for cloned Pod ready timeout: %v", err)
		return nil, err
	}

	//2. delete the original pod--podA
	delOpt := &metav1.DeleteOptions{}
	if err := podClient.Delete(pod.Name, delOpt); err != nil {
		glog.Warningf("Resize podContainer warning: failed to delete original pod: %v", err)
	}

	//3. add labels to podC
	xpod, err := podClient.Get(npod.Name, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Resize podContainer failed: failed to get the cloned pod: %v", err)
		return nil, err
	}

	//TODO: compare resourceVersion of xpod and npod before updating
	if (labels != nil) && len(labels) > 0 {
		xpod.Labels = labels
		if _, err := podClient.Update(xpod); err != nil {
			glog.Errorf("Resize podContainer failed: failed to update labels for cloned pod: %v", err)
			return nil, err
		}
	}

	flag = true
	return xpod, nil
}

// create a pod with new resource limit/requests
//    return false if there is no need to update resource amount
func createResizePod(client *kclient.Clientset, pod *k8sapi.Pod, spec *containerResizeSpec) (*k8sapi.Pod, bool, error) {
	id := fmt.Sprintf("%s/%s-%d", pod.Namespace, pod.Name, spec.Index)

	//1. copy pod
	npod := &k8sapi.Pod{}
	copyPodWithoutLabel(pod, npod)
	npod.Spec.NodeName = pod.Spec.NodeName
	npod.Name = genNewPodName(pod.Name)
	// this annotation can be used for future garbage collection if action is interrupted
	util.AddAnnotation(npod, TurboActionAnnotationKey, TurboResizeAnnotationValue)

	//2. resize resource limits/requests
	if flag, err := updateResourceAmount(npod, spec); err != nil {
		glog.Errorf("resizeContainer failed [%s]: failed to update container Capacity: %v", id, err)
		return nil, false, err
	} else if !flag {
		glog.V(2).Infof("resizeContainer aborted [%s]: no need to resize.", id)
		return nil, false, nil
	}

	//3. create pod
	podClient := client.CoreV1().Pods(pod.Namespace)
	rpod, err := podClient.Create(npod)
	if err != nil {
		glog.Errorf("Failed to create a new pod: %s/%s, %v", npod.Namespace, npod.Name, err)
		return nil, true, err
	}

	glog.V(3).Infof("Create a clone pod success: %s/%s", npod.Namespace, npod.Name)
	glog.V(4).Infof("New pod info: %++v", rpod)

	return rpod, true, nil
}
