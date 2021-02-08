package executor

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	podutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	commonutil "github.com/turbonomic/kubeturbo/pkg/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/util/retry"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "k8s.io/client-go/kubernetes"
)

//TODO: if pod is from controller, then copy pod in the way as
// kubernetes/pkg/controller/controller_utils.go#GetPodFromTemplate
// https://github.com/kubernetes/kubernetes/blob/0c7e7ae1d9cccd0cca7313ee5a8ae3c313b72139/pkg/controller/controller_utils.go#L553
func copyPodInfo(oldPod, newPod *api.Pod, copySpec bool) {
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
	if copySpec {
		//3. podSpec
		spec := oldPod.Spec
		spec.Hostname = ""
		spec.Subdomain = ""
		spec.NodeName = ""

		newPod.Spec = spec
	}

	return
}

func copyPodWithoutLabel(oldPod, newPod *api.Pod, copySpec bool) {
	copyPodInfo(oldPod, newPod, copySpec)

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
	parentKind, parentName string, retryNum int, failVolumePodMoves bool) (*api.Pod, error) {
	podClient := clusterScraper.Clientset.CoreV1().Pods(pod.Namespace)
	podQualifiedName := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	podUsingVolume := isPodUsingVolume(pod)
	if podUsingVolume && failVolumePodMoves {
		return nil, fmt.Errorf("move pod failed: Pod %s uses a persistent volume. "+
			"Set kubeturbo flag fail-volume-pod-moves to false to enable such moves.", podQualifiedName)
	}

	// We still support replicaset and replication controllers as parents,
	// however both of them could further have a parent (deploy or deploy config).
	parent, grandParent, pClient, gPClient, gPKind, err :=
		getPodOwnersInfo(clusterScraper, pod, parentKind)
	if err != nil {
		return nil, err
	}

	//NOTE: do deep-copy if the original pod may be modified outside this function
	labels := pod.Labels

	parentForPodSpec := grandParent
	if parentForPodSpec == nil {
		parentForPodSpec = parent
	}

	quotaAccessor := NewQuotaAccessor(clusterScraper.Clientset, pod.Namespace)
	quotas, err := quotaAccessor.Get()
	if err != nil {
		return nil, err
	}
	if err := quotaAccessor.Evaluate(quotas, pod); err != nil {
		return nil, err
	}

	defer quotaAccessor.Revert()
	if err := quotaAccessor.Update(); err != nil {
		return nil, err
	}

	//step A1/B1. create a clone pod--podC of the original pod--podA
	npod, err := createClonePod(clusterScraper.Clientset, pod, parentForPodSpec, nodeName)
	if err != nil {
		glog.Errorf("Move pod failed: failed to create a clone pod: %v", err)
		return nil, err
	}

	var podList *api.PodList
	//delete the clone pod if any of the below action fails
	flag := false
	defer func() {
		if !flag {
			glog.Errorf("Move pod failed, begin to delete cloned pod: %v/%v", npod.Namespace, npod.Name)
			podClient.Delete(context.TODO(), npod.Name, metav1.DeleteOptions{})
		}
		if podUsingVolume {
			// A failure can leave a pod (or pods) in pending state, delete them.
			// This can happen in case the newly created pod did fail to reach ready
			// state for whatever reason. After timeout we will end up deleting the
			// newly created pod. The invalid pod (or pods) would have been created
			// for when the parent's scheduler was set to invalid scheduler (turbo-scheduler)
			// and at exactly the same time the total replica count dropped below
			// desired count (for  whatever reason).
			deleteInvalidPendingPods(parent, podClient, podList)
		}
	}()

	// Special handling for pods with volumes
	if podUsingVolume {
		if grandParent != nil {
			pause := true
			unpause := false
			// step B8: (via defer)
			defer func() {
				glog.V(3).Infof("Unpausing pods controller: %s for pod: %s", gPKind, podQualifiedName)
				// We conservatively try additional 3 times in case of any failure as we don't
				// want the parent to be left in a bad state.
				err := commonutil.RetryDuring(defaultRetryLess, defaultRetryShortTimeout,
					defaultRetrySleepInterval, func() error {
						return resourceRollout(gPClient, grandParent, unpause)
					})
				if err != nil {
					glog.Errorf("Move pod warning: %v", err)
				}
			}()

			// step B2:
			glog.V(3).Infof("Pausing pods controller: %s for pod: %s", gPKind, podQualifiedName)
			err := resourceRollout(gPClient, grandParent, pause)
			if err != nil {
				return nil, err
			}
		}

		podList, err = parentsPods(parent, podClient)
		if err != nil {
			return nil, err
		}

		valid := true
		invalid := false
		// step B7: (via defer)
		defer func() {
			glog.V(3).Infof("Updating scheduler of %s for pod %s back to default.", parentKind, podQualifiedName)
			// We conservatively try additional 3 times in case of any failure as we don't
			// want the parent to be left in a bad state.
			err := commonutil.RetryDuring(defaultRetryLess, defaultRetryShortTimeout,
				defaultRetrySleepInterval, func() error {
					return changeScheduler(pClient, parent, valid)
				})
			if err != nil {
				glog.Errorf("Move pod warning: %v", err)
			}

		}()
		// step B3:
		glog.V(3).Infof("Updating scheduler of %s for pod %s to unknown (turbo-scheduler).", parentKind, pod.Name)
		err = changeScheduler(pClient, parent, invalid)
		if err != nil {
			return nil, err
		}

		// step A3/B4:
		// We optimistically delete the original pod (PodA), so that the new pod can
		// bind to the volume.
		// TODO: This is not an ideal way of doing things, and users should be
		// encouraged to rather disable move actions on pods which use volumes
		// via a config either in kubeturbo or driven from server UI.
		if err := podClient.Delete(context.TODO(), pod.Name, metav1.DeleteOptions{}); err != nil {
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
		if err := podClient.Delete(context.TODO(), pod.Name, metav1.DeleteOptions{}); err != nil {
			glog.Errorf("Move pod warning: failed to delete original pod: %v", err)
			return nil, err
		}
	}

	// step A4/B6: add labels to podC and remove garbage collection annotation
	xpod, err := podClient.Get(context.TODO(), npod.Name, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Move pod failed: failed to get the cloned pod: %v", err)
		return nil, err
	}

	//TODO: compare resourceVersion of xpod and npod before updating
	if (labels != nil) && len(labels) > 0 {
		// This should overwrite the garbage collection label with the original pod's labels
		xpod.Labels = labels
	} else {
		// This would remove the GC label if we are moving a standalone pod, simply because
		// a workloads pod would have labels almost always.
		xpod.Labels = make(map[string]string)
	}
	if _, err := podClient.Update(context.TODO(), xpod, metav1.UpdateOptions{}); err != nil {
		glog.Errorf("Move pod failed: failed to update labels on the cloned pod: %v", err)
		return nil, err
	}

	flag = true
	return xpod, nil
}

func deleteInvalidPendingPods(parent *unstructured.Unstructured, podClient v1.PodInterface,
	podList *api.PodList) {
	if parent == nil {
		return
	}
	// Fetch the parents pods again
	newPodList, err := parentsPods(parent, podClient)
	if err != nil {
		glog.Errorf("Error getting pod list for %s: %s: %v.", parent.GetKind(), parent.GetName(), err)
		return
	}

	// Find the pods which weren't there and if attributed to invalid
	// turbo-scheduler, delete them.
	for _, pod := range newPendingInvalidPods(newPodList, podList) {
		err := commonutil.RetryDuring(defaultRetryLess, defaultRetryShortTimeout,
			defaultRetrySleepInterval, func() error {
				return podClient.Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			})
		if err != nil {
			glog.Errorf("Failed to delete pending pod: %s.", pod.Name)
		}
	}

}

func newPendingInvalidPods(newList, oldList *api.PodList) []*api.Pod {
	var diffList []*api.Pod
	if newList == nil {
		return diffList
	}
	for _, pod1 := range newList.Items {
		found := false
		if oldList != nil {
			for _, pod2 := range oldList.Items {
				if pod1.Name == pod2.Name {
					// pod1 from newList also exists in oldList
					found = true
				}
			}
		}
		if found == true {
			continue
		}
		if pod1.Status.Phase == api.PodPending &&
			pod1.Spec.SchedulerName == DummyScheduler {
			diffList = append(diffList, &pod1)
		}
	}

	return diffList
}

func parentsPods(parent *unstructured.Unstructured, podClient v1.PodInterface) (*api.PodList, error) {
	kind, objName := parent.GetKind(), parent.GetName()
	selectorUnstructured, found, err := unstructured.NestedFieldCopy(parent.Object, "spec", "selector")
	if err != nil || !found {
		return nil, fmt.Errorf("error retrieving selectors from %s %s: %v", kind, objName, err)
	}

	selectorString := ""
	conversionError := fmt.Errorf("error converting unstructured selectors to typed selectors for %s %s: %v", kind, objName, err)
	if kind == commonutil.KindReplicationController {
		// Deployment config and ReplicationControllers define selectors as map[string]string
		// unlike what label selector has evolved as (metav1.LabelSelector{}) upstream.
		selectors := map[string]string{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(selectorUnstructured.(map[string]interface{}), &selectors); err != nil {
			return nil, conversionError
		}
		selectorString = formatStringMapSelector(selectors)
	} else {
		selectors := metav1.LabelSelector{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(selectorUnstructured.(map[string]interface{}), &selectors); err != nil {
			return nil, conversionError
		}
		selectorString = metav1.FormatLabelSelector(&selectors)
	}

	listOpts := metav1.ListOptions{LabelSelector: selectorString}
	podList, err := podClient.List(context.TODO(), listOpts)
	if err != nil {
		return nil, err
	}

	return podList, nil
}

func formatStringMapSelector(selectors map[string]string) string {
	s := []string{}
	for key, val := range selectors {
		s = append(s, fmt.Sprintf("%s=%s", key, val))
	}
	return strings.Join(s, ",")
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

// getPodOwnersInfo gets the pods owner objects (deployment, replicaset, et al)
// and the client interfaces to make updates to the objects.
// TODO: this piece of code can be cleaned up and can be made generic to return
// any level of owners in a slice. So if there are three owner levels, for example
// a replicaset, which is controlled by a deployment which is controlled by an operator
// a single iterative function will collect all of them and return all in a slice.
func getPodOwnersInfo(clusterScraper *cluster.ClusterScraper, pod *api.Pod,
	parentKind string) (*unstructured.Unstructured, *unstructured.Unstructured,
	dynamic.ResourceInterface, dynamic.ResourceInterface, string, error) {
	gPkind, name, _, parent, nsParentClient, err := clusterScraper.GetPodGrandparentInfo(pod, true)
	if err != nil {
		return nil, nil, nil, nil, gPkind, fmt.Errorf("error getting pods final owner: %v", err)
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
			err = fmt.Errorf("unsupported pods controller kind: %s while moving pod", gPkind)
			return nil, nil, nil, nil, gPkind, err
		}

		nsGpClient := clusterScraper.DynamicClient.Resource(res).Namespace(pod.Namespace)
		gParent, err := nsGpClient.Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil || gParent == nil {
			return nil, nil, nil, nil, gPkind, fmt.Errorf("failed to get pods controller: %s[%v/%v]: %v",
				gPkind, pod.Namespace, name, err)
		}

		// As of now we only have XL as the example for operator controlled resources (deployment) which
		// has pods using volumes. Seemingly this operator ignores the "paused" field of the deployments
		// while reconcoling the resources therefor enabling us to actually be able to move the pods
		// without the need to update it via the operators custom resource.
		// We therefor do not fail here and let the move happen as it would be when the resource
		// is not operator controlled.

		// Actually a parent, eg. a replicaset, although unlikely can also be controlled by an operator
		// but we ignore that case for now as we do not have an example of that in any env yet.
		// TODO: add support for such (viz replicaset) operator managed resources.
		/*if len(gParent.GetOwnerReferences()) > 0 {
			return nil, nil, nil, nil, gPkind, fmt.Errorf("the parent's parent is probably controlled by an operator. "+
				"Failing podmove for %s[%v/%v]: %v", gPkind, pod.Namespace, name, err)
		}*/
		return parent, gParent, nsParentClient, nsGpClient, gPkind, nil
	}

	// This means that the parent itself is the final owner and there is
	// no controller controlling the parent, i.e. no grand parent.
	return parent, nil, nsParentClient, nil, "", nil
}

// resourceRollout pauses/unpauses the rollout of a deployment or a deploymentconfig
func resourceRollout(client dynamic.ResourceInterface, obj *unstructured.Unstructured, pause bool) error {
	kind := obj.GetKind()
	name := obj.GetName()
	// This takes care of conflicting updates for example by operator
	// Ref: https://github.com/kubernetes/client-go/blob/master/examples/dynamic-create-update-delete-deployment/main.go
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		objCopy, err := client.Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if err := unstructured.SetNestedField(objCopy.Object, pause, "spec", "paused"); err != nil {
			return err
		}
		_, err = client.Update(context.TODO(), objCopy, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		return nil
	})

	if retryErr != nil {
		return fmt.Errorf("error setting 'spec.paused' to %t for %s %s: %v", pause, kind, name, retryErr)
	}
	return nil
}

// changeScheduler sets the scheduler value to default or unsets it to an invalid one
func changeScheduler(client dynamic.ResourceInterface, obj *unstructured.Unstructured, valid bool) error {
	kind := obj.GetKind()
	name := obj.GetName()
	// This takes care of conflicting updates for example by operator
	// Ref: https://github.com/kubernetes/client-go/blob/master/examples/dynamic-create-update-delete-deployment/main.go
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		objCopy, err := client.Get(context.TODO(), name, metav1.GetOptions{})
		if valid {
			if err := unstructured.SetNestedField(objCopy.Object, "default-scheduler",
				"spec", "template", "spec", "schedulerName"); err != nil {
				return err
			}
		} else {
			if err := unstructured.SetNestedField(objCopy.Object, DummyScheduler,
				"spec", "template", "spec", "schedulerName"); err != nil {
				return err
			}
		}

		_, err = client.Update(context.TODO(), objCopy, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		return nil
	})

	if retryErr != nil {
		return fmt.Errorf("error updating scheduler [valid=%t] for %s %s: %v", valid, kind, name, retryErr)
	}
	return nil
}

func createClonePod(client *kclient.Clientset, pod *api.Pod, parent *unstructured.Unstructured, nodeName string) (*api.Pod, error) {
	npod := &api.Pod{}
	copyPodWithoutLabel(pod, npod, false)

	// copy pod spec, annotations, and labels from the parent
	kind := parent.GetKind()
	parentName := parent.GetName()
	// pod spec
	podSpecUnstructured, found, err := unstructured.NestedFieldCopy(parent.Object, "spec", "template", "spec")
	if err != nil || !found {
		return nil, fmt.Errorf("movePod: error retrieving podSpec from %s %s: %v", kind, parentName, err)
	}
	podSpec := api.PodSpec{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(podSpecUnstructured.(map[string]interface{}), &podSpec); err != nil {
		return nil, fmt.Errorf("movePod: error converting unstructured pod spec to typed pod spec for %s %s: %v", kind, parentName, err)
	}
	// annotations
	annotations := make(map[string]string)
	annotationsUnstructured, found, err := unstructured.NestedFieldCopy(parent.Object, "spec", "template", "metadata", "annotations")
	if err != nil {
		return nil, fmt.Errorf("movePod: error retrieving annotations from %s %s: %v", kind, parentName, err)
	}
	if found {
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(annotationsUnstructured.(map[string]interface{}), &annotations); err != nil {
			return nil, fmt.Errorf("movePod: error converting unstructured annotations to typed annotations for %s %s: %v", kind, parentName, err)
		}
	}

	// Set podSpec retrieved from parent. This saves from landing into
	// problems of sidecar containers and injection systems.
	npod.Spec = podSpec
	npod.Spec.NodeName = nodeName
	npod.Name = genNewPodName(pod)
	npod.Annotations = annotations
	// this annotation can be used for future garbage collection if action is interrupted
	util.AddAnnotation(npod, TurboActionAnnotationKey, TurboMoveAnnotationValue)
	// This label is used for garbage collection if a given action leaks this pod.
	util.AddLabel(npod, TurboGCLabelKey, TurboGCLabelVal)

	podClient := client.CoreV1().Pods(pod.Namespace)
	rpod := &api.Pod{}
	err = wait.PollImmediate(defaultRetrySleepInterval, defaultRetryShortTimeout, func() (bool, error) {
		rpod, err = podClient.Create(context.TODO(), npod, metav1.CreateOptions{})
		if err != nil {
			// The quota update might not reflect in the admission controller cache immediately
			// which can still fail the new pod creation even after the quota update.
			// We retry for a short while before failing in such cases.
			if apierrors.IsForbidden(err) && strings.Contains(err.Error(), "exceeded quota") {
				return false, nil
			}

			return false, err
		}
		return true, nil
	})
	if err != nil {
		glog.Errorf("Failed to create a new pod: %s/%s, %v", npod.Namespace, npod.Name, err)
		return nil, err
	}

	glog.V(3).Infof("Create a clone pod success: %s/%s", npod.Namespace, npod.Name)
	glog.V(4).Infof("New pod info: %++v", rpod)

	return rpod, nil
}
