package executor

import (
	"fmt"
	"k8s.io/client-go/dynamic"
	"math"

	"github.com/golang/glog"

	"github.com/turbonomic/kubeturbo/pkg/action/util"
	podutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	k8sapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "k8s.io/client-go/kubernetes"
)

// update the Pod.Containers[index]'s Resources.Requests
func updateReservation(container *k8sapi.Container, patchReservation k8sapi.ResourceList) bool {
	glog.V(4).Infof("Begin to update Request(Reservation).")
	changed := false

	//1. get the original requests
	result := make(k8sapi.ResourceList)
	for k, v := range container.Resources.Requests {
		result[k] = v
	}

	//2. apply the patch
	for k, v := range patchReservation {
		oldv, exist := result[k]
		if !exist || oldv.Cmp(v) != 0 {
			result[k] = v
			changed = true
		}
	}

	if changed {
		glog.V(2).Infof("Try to update container %v resource request from %+v to %+v",
			container.Name, container.Resources.Requests, result)
		container.Resources.Requests = result
	}
	return changed
}

// update the Pod.Containers[index]'s Resources.Limits
func updateCapacity(container *k8sapi.Container, patchCapacity k8sapi.ResourceList) bool {
	glog.V(4).Infof("Begin to update Capacity.")
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

	if changed {
		glog.V(2).Infof("Try to update container %v resource limit from %+v to %v",
			container.Name, container.Resources.Limits, result)
		container.Resources.Limits = result
	}

	return changed
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
			return fmt.Errorf("resource request is larger than limits: %v %v Vs. %v",
				k.String(), v.String(), rv.String())
		}
	}

	return nil
}

func updateResourceAmount(podSpec *k8sapi.PodSpec, spec *containerResizeSpec) (bool, error) {
	//1. get container
	index := spec.Index
	if index >= len(podSpec.Containers) {
		return false, fmt.Errorf("failed to find container[%d] in pod", index)
	}
	container := &(podSpec.Containers[index])

	//2. update Limits
	changed := false
	if spec.NewCapacity != nil && len(spec.NewCapacity) > 0 {
		changed = changed || updateCapacity(container, spec.NewCapacity)
	}

	//3. update Requests
	if spec.NewRequest != nil && len(spec.NewRequest) > 0 {
		changed = changed || updateReservation(container, spec.NewRequest)
	}

	//4. check the new Limits vs. Requests, make sure Limits >= Requests
	if err := checkLimitsRequests(container); err != nil {
		return false, err
	}

	if !changed {
		glog.V(2).Infof("Container %v resources are not changed.", container.Name)
	}

	return changed, nil
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
	return resource.ParseQuantity(fmt.Sprintf("%dm", cpuTime))
}

// generate a resource.Quantity for Memory
func genMemoryQuantity(newValue float64) (resource.Quantity, error) {
	tmp := int64(newValue)
	if tmp < 1 {
		tmp = 1
	}
	return resource.ParseQuantity(fmt.Sprintf("%dKi", tmp))
}

func resizeContainer(client *kclient.Clientset, dynClient dynamic.Interface, pod *k8sapi.Pod, spec *containerResizeSpec,
	consistentResize bool) (*k8sapi.Pod, error) {
	if consistentResize {
		return nil, resizeControllerContainer(client, dynClient, pod, spec)
	}
	return resizeSingleContainer(client, pod, spec)
}

// resizeControllerContainer updates the pod template of the controller that this container pod
// belongs to. The behavior is different for different controllers:
// - For Deployment, after pod template is successfully updated, a new ReplicaSet will be created
//   and associated with this Deployment. After the new ReplicaSet scales to the desired number of
//   pods, the original ReplicaSet associated with this Deployment is then scaled to 0, effectively
//   terminating all original pods
// - For ReplicaSet and ReplicationController, only pod template will be updated with the new
//   resource, all existing pods that belong to the original ReplicaSet and ReplicationController
//   are not affected. Only newly created pods (through scaling action) will use the updated
//   resource
func resizeControllerContainer(client *kclient.Clientset, dynClient dynamic.Interface, pod *k8sapi.Pod, spec *containerResizeSpec) error {
	// prepare controllerUpdater
	controllerUpdater, err := newK8sControllerUpdater(client, dynClient, pod)
	if err != nil {
		glog.Errorf("Failed to create controllerUpdater: %v", err)
		return err
	}
	glog.V(2).Infof("Begin to consistently resize %v of pod %s/%s.",
		controllerUpdater.controller, pod.Namespace, pod.Name)
	// execute the action to update resource requirements of the container of interest
	err = controllerUpdater.updateWithRetry(&controllerSpec{0, spec})
	if err != nil {
		glog.Errorf("Failed to consistently resize %v of pod %s/%s: %v",
			controllerUpdater.controller, pod.Namespace, pod.Name, err)
		return err
	}
	return nil
}

// resizeSingleContainer resizes a single container pod in the following steps:
// - create a clone pod of the original pod (without labels), with new resource limits/requests;
// - wait until the cloned pod is ready
// - delete the original pod
// - add the labels to the cloned pod
// If the action fails, the cloned pod will be deleted
func resizeSingleContainer(client *kclient.Clientset, originalPod *k8sapi.Pod, spec *containerResizeSpec) (*k8sapi.Pod, error) {
	// check parent controller of the original pod
	fullName := util.BuildIdentifier(originalPod.Namespace, originalPod.Name)
	parentKind, parentName, _, err := podutil.GetPodParentInfo(originalPod)
	if err != nil {
		glog.Errorf("Resize action failed: failed to get pod[%s] parent info: %v.", fullName, err)
		return nil, err
	}
	if !util.SupportedParent(parentKind) {
		err = fmt.Errorf("parent kind %v is not supported", parentKind)
		glog.Errorf("Resize action aborted: %v.", err)
		return nil, err
	}

	id := fmt.Sprintf("%s/%s-%d", originalPod.Namespace, originalPod.Name, spec.Index)
	if parentKind == "" {
		glog.V(2).Infof("Begin to resize bare pod container[%s].", id)
	} else {
		glog.V(2).Infof("Begin to resize container[%s] parent=%s/%s.",
			id, parentKind, parentName)
	}

	// Make sure we can get the pod client
	podClient := client.CoreV1().Pods(originalPod.Namespace)
	if podClient == nil {
		err := fmt.Errorf("failed to get pod client for namespace: %v", originalPod.Namespace)
		glog.Errorf("Failed to resize container %s: %v", id, err)
		return nil, err
	}

	// create a clone pod with new size
	clonePod, changed, err := clonePodWithNewSize(client, originalPod, spec)
	if err != nil {
		glog.Errorf("Failed to clone pod %s with new size: %v", id, err)
		return nil, err
	}

	if !changed {
		glog.Warningf("No need to resize container %s. Not enough change.", id)
		return nil, fmt.Errorf("resize aborted due to not enough change")
	}

	// delete the clone pod if this action fails
	success := false
	defer func() {
		if !success {
			glog.Errorf("Failed to resize container %v, begin to delete cloned pod: %v/%v.",
				id, clonePod.Namespace, clonePod.Name)
			if err := podClient.Delete(clonePod.Name, &metav1.DeleteOptions{}); err != nil {
				glog.Warningf("Failed to delete cloned pod %v/%v after resize container %v has failed.",
					clonePod.Namespace, clonePod.Name, id)
			}
		}
	}()

	// wait until the clone pod gets ready
	err = podutil.WaitForPodReady(client, clonePod.Namespace, clonePod.Name, "", defaultRetryMore, defaultPodCreateSleep)
	if err != nil {
		glog.Errorf("Wait for cloned Pod ready timeout: %v", err)
		return nil, err
	}

	// delete the original pod after the clone pod is ready
	delOpt := &metav1.DeleteOptions{}
	if err := podClient.Delete(originalPod.Name, delOpt); err != nil {
		glog.Warningf("Resize podContainer warning: failed to delete original pod: %v", err)
	}

	// add labels to the clone pod so it can be attached to the controller if any
	xpod, err := podClient.Get(clonePod.Name, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Resize podContainer failed: failed to get the cloned pod: %v", err)
		return nil, err
	}
	//TODO: compare resourceVersion of xpod and npod before updating
	if len(originalPod.Labels) > 0 {
		xpod.Labels = originalPod.Labels
		if _, err := podClient.Update(xpod); err != nil {
			glog.Errorf("Resize podContainer failed: failed to update labels for cloned pod: %v", err)
			return nil, err
		}
	}
	// success!
	success = true
	return xpod, nil
}

// clonePodWithNewSize creates a pod with new resource limit/requests
// return false if there is no need to update resource amount
func clonePodWithNewSize(client *kclient.Clientset, pod *k8sapi.Pod, spec *containerResizeSpec) (*k8sapi.Pod, bool, error) {
	id := fmt.Sprintf("%s/%s-%d", pod.Namespace, pod.Name, spec.Index)

	//1. copy pod
	npod := &k8sapi.Pod{}
	copyPodWithoutLabel(pod, npod)
	npod.Spec.NodeName = pod.Spec.NodeName
	npod.Name = genNewPodName(pod)
	// this annotation can be used for future garbage collection if action is interrupted
	util.AddAnnotation(npod, TurboActionAnnotationKey, TurboResizeAnnotationValue)

	//2. resize resource limits/requests
	glog.V(4).Infof("Update container %v resources in the pod specification.", id)
	changed, err := updateResourceAmount(&npod.Spec, spec)
	if err != nil {
		return nil, false, fmt.Errorf("failed to update capacity for container %s: %v", id, err)
	}
	if !changed {
		return nil, false, nil
	}

	//3. create pod
	podClient := client.CoreV1().Pods(pod.Namespace)
	rpod, err := podClient.Create(npod)
	if err != nil {
		return nil, true, err
	}

	glog.V(3).Infof("Create a clone pod success: %s/%s", npod.Namespace, npod.Name)
	glog.V(4).Infof("New pod info: %+v", rpod)

	return rpod, true, nil
}
