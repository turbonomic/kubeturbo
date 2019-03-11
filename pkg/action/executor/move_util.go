package executor

import (
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	podutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "k8s.io/client-go/kubernetes"
)

//TODO: if pod is from controller, then copy pod in the way as
// kubernetes/pkg/controller/controller_utils.go#GetPodFromTemplate
// https://github.com/kubernetes/kubernetes/blob/0c7e7ae1d9cccd0cca7313ee5a8ae3c313b72139/pkg/controller/controller_utils.go#L553
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

func copyPodWithoutLabel(oldPod, newPod *api.Pod) {
	copyPodInfo(oldPod, newPod)

	// set Labels and OwnerReference to be empty
	newPod.Labels = make(map[string]string)
	newPod.OwnerReferences = []metav1.OwnerReference{}
}

// Generates a name for the new pod from the old one. The new pod name will
// be the original pod name followed by "-" + current timestamp.
func genNewPodName(oldPod *api.Pod) string {
	oldPodName := oldPod.Name
	oriPodName := oldPodName

	// If the pod was created from Turbo actions (resize/move), the oldPodName
	// will include its timestamp. In such case, we want to find the original
	// pod name.
	if _, ok := oldPod.Annotations[TurboActionAnnotationKey]; ok {
		if idx := strings.LastIndex(oldPodName, "-"); idx >= 0 {
			oriPodName = oldPodName[:idx]
		}
	}

	// Append the pod name with current timestamp
	newPodName := oriPodName + "-" + strconv.FormatInt(time.Now().UnixNano(), 32)
	glog.V(4).Infof("Generated new pod name %s for pod %s (original pod %s)", newPodName, oldPodName, oriPodName)

	return newPodName
}

// Move pod to node nodeName in four steps:
//  step1: create a clone pod of the original pod (without labels)
//  step2: wait until the cloned pod is ready
//  step3: delete the original pod
//  step4: add the labels to the cloned pod
func movePod(client *kclient.Clientset, pod *api.Pod, nodeName string, retryNum int) (*api.Pod, error) {
	podClient := client.CoreV1().Pods(pod.Namespace)
	//NOTE: do deep-copy if the original pod may be modified outside this function
	labels := pod.Labels

	//1. create a clone pod--podC of the original pod--podA
	npod, err := createClonePod(client, pod, nodeName)
	if err != nil {
		glog.Errorf("Move pod failed: failed to create a clone pod: %v", err)
		return nil, err
	}

	//delete the clone pod if this action fails
	flag := false
	defer func() {
		if !flag {
			glog.Errorf("Move pod failed, begin to delete cloned pod: %v/%v", npod.Namespace, npod.Name)
			delOpt := &metav1.DeleteOptions{}
			podClient.Delete(npod.Name, delOpt)
		}
	}()

	//2 wait until podC gets ready
	err = podutil.WaitForPodReady(client, npod.Namespace, npod.Name, nodeName,
		retryNum, defaultPodCreateSleep)
	if err != nil {
		glog.Errorf("Wait for cloned Pod ready timeout: %v", err)
		return nil, err
	}

	//3. delete the original pod--podA
	delOpt := &metav1.DeleteOptions{}
	if err := podClient.Delete(pod.Name, delOpt); err != nil {
		glog.Errorf("Move pod warning: failed to delete original pod: %v", err)
		return nil, err
	}

	//4. add labels to podC
	xpod, err := podClient.Get(npod.Name, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Move pod failed: failed to get the cloned pod: %v", err)
		return nil, err
	}

	//TODO: compare resourceVersion of xpod and npod before updating
	if (labels != nil) && len(labels) > 0 {
		xpod.Labels = labels
		if _, err := podClient.Update(xpod); err != nil {
			glog.Errorf("Move pod failed: failed to update labels for cloned pod: %v", err)
			return nil, err
		}
	}

	flag = true
	return xpod, nil
}

func createClonePod(client *kclient.Clientset, pod *api.Pod, nodeName string) (*api.Pod, error) {
	npod := &api.Pod{}
	copyPodWithoutLabel(pod, npod)
	npod.Spec.NodeName = nodeName
	npod.Name = genNewPodName(pod)
	// this annotation can be used for future garbage collection if action is interrupted
	util.AddAnnotation(npod, TurboActionAnnotationKey, TurboMoveAnnotationValue)

	podClient := client.CoreV1().Pods(pod.Namespace)
	rpod, err := podClient.Create(npod)
	if err != nil {
		glog.Errorf("Failed to create a new pod: %s/%s, %v", npod.Namespace, npod.Name, err)
		return nil, err
	}

	glog.V(3).Infof("Create a clone pod success: %s/%s", npod.Namespace, npod.Name)
	glog.V(4).Infof("New pod info: %++v", rpod)

	return rpod, nil
}
