package executor

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	podutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	commonutil "github.com/turbonomic/kubeturbo/pkg/util"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

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
//  stepA1: create a clone pod of the original pod (without labels)
//  stepA2: wait until the cloned pod is ready
//  stepA3: delete the original pod
//  stepA4: add the labels to the cloned pod
// If a pod has a persistent volume attached the steps are different:
//  stepB1: create a clone pod of the original pod (without labels)
//  stepB2: if the parent has parent (rs has deployment) pause the rollout
//  stepB3: change the scheduler of parent to non-default (turbo-scheduler)
//  stepB4: delete the original pod
//  stepB5: wait until the cloned pod is ready
//  stepB6: add the labels to the cloned pod
//  stepB7: change the scheduler of parent back to to default-scheduler
//  stepB8: if the parent has parent, unpause the rollout
// TODO: add support for operator controlled parent or parent's parent.
func movePod(clusterScraper *cluster.ClusterScraper, pod *api.Pod, nodeName,
	parentKind, parentName string, retryNum int) (*api.Pod, error) {
	podClient := clusterScraper.Clientset.CoreV1().Pods(pod.Namespace)
	//NOTE: do deep-copy if the original pod may be modified outside this function
	labels := pod.Labels

	//step A1/B1. create a clone pod--podC of the original pod--podA
	npod, err := createClonePod(clusterScraper.Clientset, pod, nodeName)
	if err != nil {
		glog.Errorf("Move pod failed: failed to create a clone pod: %v", err)
		return nil, err
	}

	//delete the clone pod if any of the below action fails
	flag := false
	defer func() {
		if !flag {
			glog.Errorf("Move pod failed, begin to delete cloned pod: %v/%v", npod.Namespace, npod.Name)
			delOpt := &metav1.DeleteOptions{}
			podClient.Delete(npod.Name, delOpt)
		}
	}()

	podUsingVolume := isPodUsingVolume(pod)
	// Special handling for pods with volumes
	if podUsingVolume {
		// We still support replicaset and replication controllers as parents,
		// however both of them could further have a parent (deploy or deploy config).
		parent, grandParent, pClient, gPClient, gPKind, err :=
			getParentAndGrandParentInfo(clusterScraper, pod, parentKind)
		if err != nil {
			return nil, err
		}

		if grandParent != nil {
			pause := true
			unpause := false
			// step B8: (via defer)
			defer func() {
				glog.V(3).Infof("Unpausing pods controller: %s for pod: %s", gPKind, pod.Name)
				resourceRollout(gPClient, grandParent, unpause)
			}()

			// step B2:
			glog.V(3).Infof("Pausing pods controller: %s for pod: %s", gPKind, pod.Name)
			err := resourceRollout(gPClient, grandParent, pause)
			if err != nil {
				return nil, err
			}
		}

		valid := true
		invalid := false
		// step B7: (via defer)
		defer func() {
			glog.V(3).Infof("Updating scheduler of %s for pod %s back to default.", parentKind, pod.Name)
			changeScheduler(pClient, parent, valid)

		}()
		// step B3:
		glog.V(3).Infof("Updating scheduler of %s for pod %s to unknown (turbo-scheduler).", parentKind, pod.Name)
		changeScheduler(pClient, parent, invalid)

		// step A3/B4:
		// We optimistically delete the original pod (PodA), so that the new pod can
		// bind to the volume.
		// TODO: This is not an ideal way of doing things, and users should be
		// encouraged to rather disable move actions on pods which use volumes
		// via a config either in kubeturbo or driven from server UI.
		delOpt := &metav1.DeleteOptions{}
		if err := podClient.Delete(pod.Name, delOpt); err != nil {
			glog.Errorf("Move pod warning: failed to delete original pod: %v", err)
			return nil, err
		}
	}

	//step A2/B5: wait until podC gets ready
	err = podutil.WaitForPodReady(clusterScraper.Clientset, npod.Namespace, npod.Name, nodeName,
		retryNum, defaultPodCreateSleep)
	if err != nil {
		glog.Errorf("Wait for cloned Pod ready timeout: %v", err)
		return nil, err
	}

	if !podUsingVolume {
		// step A3: delete the original pod--podA
		delOpt := &metav1.DeleteOptions{}
		if err := podClient.Delete(pod.Name, delOpt); err != nil {
			glog.Errorf("Move pod warning: failed to delete original pod: %v", err)
			return nil, err
		}
	}

	// step A4/B6: add labels to podC
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

func isPodUsingVolume(pod *api.Pod) bool {
	for _, vol := range pod.Spec.Volumes {
		// Exactly one of these would be non nil at a given point in time.
		if vol.PersistentVolumeClaim != nil ||
			// We do not currently have a mechanism to test below.
			// Also the preferred way by any provider is now anyways
			// via a persistent volume claim.
			vol.GCEPersistentDisk != nil ||
			vol.AWSElasticBlockStore != nil ||
			vol.AzureDisk != nil ||
			vol.CephFS != nil ||
			vol.Cinder != nil ||
			vol.PortworxVolume != nil ||
			vol.StorageOS != nil ||
			vol.VsphereVolume != nil {
			return true
		}
	}
	return false
}

func getParentAndGrandParentInfo(clusterScraper *cluster.ClusterScraper, pod *api.Pod,
	parentKind string) (*unstructured.Unstructured, *unstructured.Unstructured,
	dynamic.ResourceInterface, dynamic.ResourceInterface, string, error) {
	gPkind, name, _, parent, nsParentClient, err := clusterScraper.GetPodGrandparentInfo(pod, false)
	if err != nil {
		return nil, nil, nil, nil, gPkind, fmt.Errorf("Error getting pods final owner: %v", err)
	}

	if gPkind != parentKind {
		var res schema.GroupVersionResource
		switch gPkind {
		case commonutil.KindDeployment:
			res = schema.GroupVersionResource{
				Group:    commonutil.K8sAPIDeploymentGV.Group,
				Version:  commonutil.K8sAPIDeploymentGV.Version,
				Resource: commonutil.DeploymentResName}
		case commonutil.KindDeploymentConfig:
			res = schema.GroupVersionResource{
				Group:    commonutil.OpenShiftAPIDeploymentConfigGV.Group,
				Version:  commonutil.OpenShiftAPIDeploymentConfigGV.Version,
				Resource: commonutil.DeploymentConfigResName}
		default:
			err = fmt.Errorf("Unsupported pods controller kind: %s while moving pod", gPkind)
			glog.Error(err.Error())
			return nil, nil, nil, nil, gPkind, err
		}

		nsGpClient := clusterScraper.DynamicClient.Resource(res).Namespace(pod.Namespace)
		gParent, err := nsGpClient.Get(name, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("Failed to get pods controller: %s[%v/%v]: %v", gPkind, pod.Namespace, name, err)
			return nil, nil, nil, nil, gPkind, err
		}
		// TODO: add support for operator owned controllers.
		// As of now we fail if we observe that grandparent has an owner (assuming its an operator)
		// Actually a parent, eg. a replicaset although unlikely can also be controlled by an operator.
		if len(gParent.GetOwnerReferences()) > 0 {
			glog.Errorf("The parent's parent is probably controlled by an operator."+
				"\nFailing podmove for %s[%v/%v]: %v", gPkind, pod.Namespace, name, err)
			return nil, nil, nil, nil, gPkind, fmt.Errorf("failing pod move")
		}
		return parent, gParent, nsParentClient, nsGpClient, gPkind, nil
	}

	// This means that the parent itself is the final owner and there is
	// no controller controlling the parent, i.e. no grand parent.
	return nil, nil, nil, nil, "", nil
}

// resourceRollout pauses/unpauses the rollout of a deployment or a deploymentconfig
func resourceRollout(client dynamic.ResourceInterface, obj *unstructured.Unstructured, pause bool) error {
	// Avoid mutating the passed object
	objCopy := obj.DeepCopy()
	kind := obj.GetKind()
	name := obj.GetName()
	if err := unstructured.SetNestedField(objCopy.Object, pause, "spec", "paused"); err != nil {
		return fmt.Errorf("error pausing %s %s: %v", kind, name, err)
	}

	_, err := client.Update(objCopy, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("error pausing %s %s: %v", kind, name, err)
	}
	return nil
}

// changeScheduler sets the scheduler value to default or unsets it to an invalid one
func changeScheduler(client dynamic.ResourceInterface, obj *unstructured.Unstructured, valid bool) error {
	objCopy := obj.DeepCopy()
	kind := obj.GetKind()
	name := obj.GetName()

	if valid {
		if err := unstructured.SetNestedField(objCopy.Object, "default-scheduler",
			"spec", "template", "spec", "schedulerName"); err != nil {
			return fmt.Errorf("error setting scheduler to default-scheduler for %s %s: %v", kind, name, err)
		}
	} else {
		if err := unstructured.SetNestedField(objCopy.Object, "turbo-scheduler",
			"spec", "template", "spec", "schedulerName"); err != nil {
			return fmt.Errorf("error setting scheduler to unknown (turbo-scheduler) for %s %s: %v", kind, name, err)
		}
	}

	_, err := client.Update(objCopy, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("error updating scheduler name for %s %s: %v", kind, name, err)
	}
	return nil
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
