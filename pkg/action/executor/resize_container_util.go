package executor

import (
	"fmt"
	"github.com/golang/glog"
	"math"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	kclient "k8s.io/client-go/kubernetes"
	k8sapi "k8s.io/client-go/pkg/api/v1"

	autil "github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/util"
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

/* make sure Capacity >= Request */
func adjustRequests(container *k8sapi.Container, limits k8sapi.ResourceList) error {
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

func resizeContainer(client *kclient.Clientset, pod *k8sapi.Pod, spec *containerResizeSpec, retryNum int) error {
	index := spec.Index
	id := fmt.Sprintf("%s/%s-%d", pod.Namespace, pod.Name, index)
	glog.V(2).Infof("begin to resize Pod container[%s].", id)

	// 0. get the latest Pod
	var err error
	if pod, err = autil.GetPod(client, pod.Namespace, pod.Name); err != nil {
		glog.Errorf("Failed to get latest pod %v: %v", id, err)
		return err
	}

	// 1. copy the original pod
	npod := &k8sapi.Pod{}
	copyPodInfo(pod, npod)
	npod.Spec.NodeName = pod.Spec.NodeName

	//2. update resource capacity
	if flag, err := updateResourceAmount(npod, spec); err != nil {
		glog.Errorf("resizeContainer failed [%s]: failed to update container Capacity: %v", id, err)
		return err
	} else if !flag {
		glog.V(2).Infof("resizeContainer aborted [%s]: no need to resize.", id)
		return nil
	}

	//3. kill the original pod
	grace := calcGracePeriod(pod)
	if err = autil.DeletePod(client, pod.Namespace, pod.Name, grace); err != nil {
		err = fmt.Errorf("resizeContainer failed [%s]: failed to delete orginial pod: %v", id, err)
		glog.Error(err)
		return err
	}

	//4. create a new pod
	// wait for the previous pod to be cleaned up.
	time.Sleep(time.Duration(grace)*time.Second + defaultMoreGrace)

	interval := defaultPodCreateSleep
	timeout := interval*time.Duration(retryNum) + time.Second*10
	err = util.RetryDuring(retryNum, timeout, interval, func() error {
		_, inerr := autil.CreatePod(client, npod)
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
