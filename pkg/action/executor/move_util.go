package executor

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/pkg/action/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	podutil "github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	commonutil "github.ibm.com/turbonomic/kubeturbo/pkg/util"
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
	restclient "k8s.io/client-go/rest"
)

// TODO: if pod is from controller, then copy pod in the way as
// kubernetes/pkg/controller/controller_utils.go#GetPodFromTemplate
// https://github.com/kubernetes/kubernetes/blob/0c7e7ae1d9cccd0cca7313ee5a8ae3c313b72139/pkg/controller/controller_utils.go#L553
func copyPodInfo(oldPod, newPod *api.Pod, copySpec bool) {
	//1. typeMeta
	newPod.TypeMeta = oldPod.TypeMeta

	//2. objectMeta
	newPod.ObjectMeta.SetNamespace(oldPod.ObjectMeta.GetNamespace())
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

		// TODO: Check why were annotations never copied over in the legacy code.
		// The spec is copied over for bare pod moves and resizes.
		// We ideally should have the annotations also copied over.
		//4. annotations
		newPod.Annotations = oldPod.Annotations
	}

	return
}

func copyPodWithoutLabel(oldPod, newPod *api.Pod) {
	copyPodInfo(oldPod, newPod, true)

	// set Labels and OwnerReference to be empty
	newPod.Labels = make(map[string]string)
	newPod.OwnerReferences = []metav1.OwnerReference{}
}

func copyPodWithoutSelectedLabels(oldPod, newPod *api.Pod, excludeKeys []string, copySpec bool) {
	copyPodInfo(oldPod, newPod, copySpec)
	// set labels excluding ownership labels (those used by replicasets and replicationcontrollers)
	newPod.Labels = excludeFromLabels(oldPod.Labels, excludeKeys)
	// set OwnerRef to be empty
	newPod.OwnerReferences = []metav1.OwnerReference{}
}

func excludeFromLabels(originalLabels map[string]string, excludeKeys []string) map[string]string {
	newLabels := make(map[string]string)
	for key, val := range originalLabels {
		if keyInKeys(key, excludeKeys) {
			continue
		}
		newLabels[key] = val
	}
	return newLabels
}

func keyInKeys(key string, keys []string) bool {
	for _, k := range keys {
		if k == key {
			return true
		}
	}
	return false
}

func generatePodHash() string {
	// sleep (with jitter, 1 to 10 milliseconds) to try to keep hashes unique
	time.Sleep(time.Duration(1+rand.Intn(10)) * time.Millisecond)
	// get the current timestamp in base 32 for the "hash"
	timestamp := strconv.FormatInt(time.Now().UnixNano(), 32)
	// grab the 5 least significant chars from the "hash"
	podHash := timestamp[len(timestamp)-5:]
	return podHash
}

func getPodOwner(pod *api.Pod) (*metav1.OwnerReference, bool) {
	if pod.OwnerReferences == nil || len(pod.OwnerReferences) != 1 || pod.OwnerReferences[0].Kind == commonutil.KindNode {
		return nil, false
	}
	return &pod.OwnerReferences[0], true
}

// Generates a new name for a moved pod by adding/replacing the pod hash in the existing name
func genNewPodName(oldPod *api.Pod) string {
	currentPodName := oldPod.Name
	podNamePrefix := currentPodName
	// If the pod has an owner, drop the previous pod hash so that it can be replaced
	// with a new one. Bare pods will append a new hash as opposed to replacing them.
	if owner, exists := getPodOwner(oldPod); exists {
		podNamePrefix = owner.Name
	}
	newPodName := podNamePrefix + "-" + generatePodHash()
	glog.V(4).Infof("Generated new Pod name %s for move (original Pod name %s)", newPodName, currentPodName)
	return newPodName

}

// Move pod to node nodeName in below steps:
// If a pod has a persistent volume attached the steps are different:
//
//	step 1: create a clone pod of the original pod (without labels)
//	step 2: if the parent has parent (rs has deployment) pause the rollout
//	step 3: change the scheduler of parent to non-default (turbo-scheduler)
//
// If a pod HAS a persistent volume attached
//
//	{
//	 step 4: delete the original pod
//	}
//
//	step 5: wait until the cloned pod is ready
//
// If the pod does NOT have persistent volume attached
//
//	{
//		 step 4: delete the original pod
//	}
//
//	step 6: add the labels to the cloned pod
//	step 7: change the scheduler of parent back to to default-scheduler
//	step 8: if the parent has parent, unpause the rollout
//
// TODO: add support for operator controlled parent or parent's parent.
func movePod(clusterScraper *cluster.ClusterScraper, pod *api.Pod, nodeName, parentKind, parentName string,
	retryNum int, failVolumePodMoves, updateQuotaToAllowMoves bool, lockMap *util.ExpirationMap) (*api.Pod, error) {
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

	if updateQuotaToAllowMoves {
		quotaAccessor := NewQuotaAccessor(clusterScraper.Clientset, pod.Namespace)
		// The accessor should get the quotas again within the lock to avoid
		// possible race conditions.
		quotas, err := quotaAccessor.Get()
		if err != nil {
			return nil, err
		}
		hasQuotas := len(quotas) > 0
		if hasQuotas {
			// If this namespace has quota we force the move actions to
			// become sequential.
			lockHelper, err := lockForQuota(pod.Namespace, lockMap)
			if err != nil {
				return nil, err
			}
			defer lockHelper.ReleaseLock()

			err = checkQuotas(quotaAccessor, pod, lockMap, 1)
			if err != nil {
				return nil, err
			}
			defer quotaAccessor.Revert()
		}
	}

	// Use an impersonation client in case SCC users are updated
	client, ok := getImpersonationClientset(clusterScraper.RestConfig, pod)
	if !ok {
		client = clusterScraper.Clientset
	}
	//step 1. create a clone pod--podC of the original pod--podA
	npod, err := createClonePod(client, pod, parentForPodSpec, updateQuotaToAllowMoves, nodeName)
	if err != nil {
		glog.Errorf("Move pod failed: failed to create a clone pod: %v", err)
		return nil, err
	}

	podClient := client.CoreV1().Pods(pod.Namespace)
	var podList *api.PodList
	//delete the clone pod if any of the below action fails
	flag := false
	defer func() {
		if !flag {
			glog.Errorf("Move pod failed, begin to delete cloned pod: %v/%v", npod.Namespace, npod.Name)
			podClient.Delete(context.TODO(), npod.Name, metav1.DeleteOptions{})
		}
		// A failure can leave a pod (or pods) in pending state, delete them.
		// This can happen in case the newly created pod did fail to reach ready
		// state for whatever reason. After timeout we will end up deleting the
		// newly created pod. The invalid pod (or pods) would have been created
		// for when the parent's scheduler was set to invalid scheduler (turbo-scheduler)
		// and at exactly the same time the total replica count dropped below
		// desired count (for  whatever reason).
		deleteInvalidPendingPods(parent, podClient, podList)
	}()

	if grandParent != nil {
		pause := true
		unpause := false
		// step 8: (via defer)
		defer func() {
			glog.V(3).Infof("Unpausing pods controller: %s for pod: %s", gPKind, podQualifiedName)
			// We conservatively try additional 3 times in case of any failure as we don't
			// want the parent to be left in a bad state.
			err := commonutil.RetryDuring(DefaultExecutionRetry, DefaultRetryShortTimeout,
				DefaultRetrySleepInterval, func() error {
					return ResourceRollout(gPClient, grandParent, unpause)
				})
			if err != nil {
				glog.Errorf("Move pod warning: %v", err)
			}
		}()

		// step 2:
		glog.V(3).Infof("Pausing pods controller: %s for pod: %s", gPKind, podQualifiedName)
		err := ResourceRollout(gPClient, grandParent, pause)
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
	// step 7: (via defer)
	defer func() {
		glog.V(3).Infof("Updating scheduler of %s for pod %s back to default.", parentKind, podQualifiedName)
		// We conservatively try additional 3 times in case of any failure as we don't
		// want the parent to be left in a bad state.
		err := commonutil.RetryDuring(DefaultExecutionRetry, DefaultRetryShortTimeout,
			DefaultRetrySleepInterval, func() error {
				return ChangeScheduler(pClient, parent, valid)
			})
		if err != nil {
			glog.Errorf("Move pod warning: %v", err)
		}

	}()
	// step 3:
	glog.V(3).Infof("Updating scheduler of %s for pod %s to unknown (turbo-scheduler).", parentKind, pod.Name)
	err = ChangeScheduler(pClient, parent, invalid)
	if err != nil {
		return nil, err
	}

	if podUsingVolume {
		// step 4:
		// We optimistically delete the original pod (PodA), so that the new pod can
		// bind to the volume.
		// TODO: This is not an ideal way of doing things, and users should be
		// encouraged to rather disable move actions on pods which use volumes
		// via a config either in kubeturbo or driven from server UI.
		glog.V(4).Infof("Pod using volume. Deleting original pod %s/%s right away", pod.Namespace, pod.Name)
		if err := podClient.Delete(context.TODO(), pod.Name, metav1.DeleteOptions{}); err != nil {
			glog.Errorf("Move pod warning: failed to delete original pod: %v", err)
			return nil, err
		}
	}
	retryInterval := defaultPodCreateSleep
	failureThreshold := int32(retryNum)
	initDelay := int32(0)
	if parentForPodSpec != nil {
		unstructuredContainers, found, err := unstructured.NestedSlice(parentForPodSpec.Object, "spec", "template", "spec", "containers")
		if err != nil || !found {
			return nil, fmt.Errorf("error retrieving containers for %s/%s because: %v", pod.Namespace, pod.Name, err)
		}
		for _, unstructuredContainer := range unstructuredContainers {
			var container api.Container
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredContainer.(map[string]interface{}), &container); err != nil {
				return nil, fmt.Errorf("error converting unstructured containers to typed containers for %s/%s because : %v", pod.Namespace, pod.Name, err)
			}
			retryInterval, failureThreshold, initDelay = calculateReadinessThreshold(container, retryInterval, failureThreshold, initDelay)
		}
	} else {
		containers := pod.Spec.Containers
		for _, container := range containers {
			retryInterval, failureThreshold, initDelay = calculateReadinessThreshold(container, retryInterval, failureThreshold, initDelay)
		}
	}
	//step 5: wait until podC gets ready
	glog.V(4).Infof("Now wait for new pod to be ready %s/%s", pod.Namespace, pod.Name)
	err = podutil.WaitForPodReady(clusterScraper.Clientset, npod.Namespace, npod.Name, nodeName, time.Second*time.Duration(initDelay),
		failureThreshold, retryInterval)
	if err != nil {
		glog.Errorf("Wait for cloned Pod ready timeout: %v", err)
		return nil, err
	}

	if !podUsingVolume {
		// step 4: delete the original pod--podA
		glog.V(4).Infof("New pod ready. Deleting original pod %s/%s", pod.Namespace, pod.Name)
		if err := podClient.Delete(context.TODO(), pod.Name, metav1.DeleteOptions{}); err != nil {
			glog.Errorf("Move pod warning: failed to delete original pod: %v", err)
			return nil, err
		}
	}

	// step 6: add labels to podC and remove garbage collection annotation
	xpod, err := podClient.Get(context.TODO(), npod.Name, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Move pod failed: failed to get the cloned pod: %v", err)
		return nil, err
	}

	//TODO: compare resourceVersion of xpod and npod before updating
	if (labels != nil) && len(labels) > 0 {
		// This should overwrite the GC label with the original pod's labels
		// This will also bring the excluded labels (excluded while creating clone pod) back
		xpod.Labels = labels
	} else {
		// This would remove the GC label if we are moving a standalone pod, simply because
		// a workloads pod would have labels almost always.
		xpod.Labels = make(map[string]string)
	}

	glog.V(4).Infof("Updating new pod labels %s/%s", xpod.Namespace, xpod.Name)
	if _, err := podClient.Update(context.TODO(), xpod, metav1.UpdateOptions{}); err != nil {
		glog.Errorf("Move pod failed: failed to update labels on the cloned pod: %v", err)
		return nil, err
	}

	flag = true
	return xpod, nil
}

func calculateReadinessThreshold(container api.Container, retryInterval time.Duration, failureThreshold int32, initDelay int32) (time.Duration, int32, int32) {
	readinessFailureThreshold, readinessInitialDelaySec, periodSec := getContainerReadinessProbeDetails(container)
	duration := time.Second * time.Duration(periodSec)
	if duration > retryInterval {
		retryInterval = duration
	}
	if readinessFailureThreshold > failureThreshold {
		failureThreshold = readinessFailureThreshold
	}
	if readinessInitialDelaySec > initDelay {
		initDelay = readinessInitialDelaySec
	}
	return retryInterval, failureThreshold, initDelay
}

func getImpersonationClientset(restConfig *restclient.Config, pod *api.Pod) (*kclient.Clientset, bool) {
	if len(commonutil.SCCMapping) < 1 {
		return nil, false
	}

	sccLevel := ""
	for key, val := range pod.GetAnnotations() {
		if key == commonutil.SCCAnnotationKey {
			sccLevel = val
		}
	}
	if sccLevel == "" {
		return nil, false
	}

	ns := commonutil.GetKubeturboNamespace()
	userName := ""
	for sccName, saName := range commonutil.SCCMapping {
		if sccName == sccLevel {
			userName = commonutil.SCCUserFullName(ns, saName)
			break
		}
	}

	if userName == "" {
		glog.Warningf("It looks like pod %s/%s doesn't have a SCC mapping and may get a different SCC after move", pod.Namespace, pod.Name)
		return nil, false
	}

	config := restclient.CopyConfig(restConfig)
	config.Impersonate.UserName = userName
	client, err := kclient.NewForConfig(config)
	if err != nil {
		glog.Errorf("Failed to create Impersonating client while moving pod %s/%s: %v", ns, pod.Name, err)
		return nil, false
	}

	glog.V(2).Infof("Using Impersonation client with username: %s while moving pod %s/%s", userName, ns, pod.Name)
	return client, true
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
	for _, podName := range newPendingInvalidPods(newPodList, podList) {
		err := commonutil.RetryDuring(DefaultExecutionRetry, DefaultRetryShortTimeout,
			DefaultRetrySleepInterval, func() error {
				glog.V(5).Infof("Deleting pending pod %s", podName)
				return podClient.Delete(context.TODO(), podName, metav1.DeleteOptions{})
			})
		if err != nil {
			glog.Errorf("Failed to delete pending pod: %s.", podName)
		}
	}

}

func newPendingInvalidPods(newList, oldList *api.PodList) []string {
	var diffList []string
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
					break
				}
			}
		}
		if found {
			continue
		}
		if pod1.Status.Phase == api.PodPending &&
			pod1.Spec.SchedulerName == DummyScheduler {
			diffList = append(diffList, pod1.Name)
			glog.V(5).Infof("Found pending pod to delete: %++v", pod1.Name)
		}
	}

	return diffList
}

func parentsPods(parent *unstructured.Unstructured, podClient v1.PodInterface) (*api.PodList, error) {
	if parent == nil {
		return nil, nil
	}
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
	gpOwnerInfo, parent, nsParentClient, err := clusterScraper.GetPodControllerInfo(pod, false)
	if err != nil {
		return nil, nil, nil, nil, "", fmt.Errorf("error getting pods final owner: %v", err)
	}

	if gpOwnerInfo.Kind != parentKind {
		var res schema.GroupVersionResource
		switch gpOwnerInfo.Kind {
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
		case commonutil.KindVirtualMachine:
			res = commonutil.OpenShiftVirtualMachineGVR
		default:
			err = fmt.Errorf("unsupported pods controller kind: %s while moving pod", gpOwnerInfo.Kind)
			return nil, nil, nil, nil, gpOwnerInfo.Kind, err
		}

		nsGpClient := clusterScraper.DynamicClient.Resource(res).Namespace(pod.Namespace)
		gParent, err := nsGpClient.Get(context.TODO(), gpOwnerInfo.Name, metav1.GetOptions{})
		if err != nil || gParent == nil {
			return nil, nil, nil, nil, gpOwnerInfo.Kind, fmt.Errorf("failed to get pods controller: %s[%v/%v]: %v",
				gpOwnerInfo.Kind, pod.Namespace, gpOwnerInfo.Name, err)
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
		return parent, gParent, nsParentClient, nsGpClient, gpOwnerInfo.Kind, nil
	}

	// This means that the parent itself is the final owner and there is
	// no controller controlling the parent, i.e. no grand parent.
	return parent, nil, nsParentClient, nil, "", nil
}

func getContainerReadinessProbeDetails(container api.Container) (failureThreshold int32, initialDelaySec int32, periodSec int32) {
	probe := container.ReadinessProbe
	if probe == nil {
		glog.V(4).Infof("Readiness probe not found for Container: %s, use default configuration instead", container.Name)
		return 0, 0, 0
	}
	return probe.FailureThreshold, probe.InitialDelaySeconds, probe.PeriodSeconds
}

// ResourceRollout pauses/unpauses the rollout of a deployment or a deploymentconfig
// This additionally adds or removes the garbage collection label on the resource
func ResourceRollout(client dynamic.ResourceInterface, obj *unstructured.Unstructured, pause bool) error {
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
		if pause {
			addGCLabel(objCopy)
		} else {
			removeGCLabel(objCopy)
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

// ChangeScheduler sets the scheduler value to default or unsets it to an invalid one
// This additionally adds or removes the garbage collection label on the resource
func ChangeScheduler(client dynamic.ResourceInterface, obj *unstructured.Unstructured, valid bool) error {
	if obj == nil {
		return nil
	}
	kind := obj.GetKind()
	name := obj.GetName()
	// This takes care of conflicting updates for example by operator
	// Ref: https://github.com/kubernetes/client-go/blob/master/examples/dynamic-create-update-delete-deployment/main.go
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		objCopy, err := client.Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if valid {
			if err := unstructured.SetNestedField(objCopy.Object, DefaultScheduler,
				"spec", "template", "spec", "schedulerName"); err != nil {
				return err
			}
			removeGCLabel(objCopy)
		} else {
			if err := unstructured.SetNestedField(objCopy.Object, DummyScheduler,
				"spec", "template", "spec", "schedulerName"); err != nil {
				return err
			}
			addGCLabel(objCopy)
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

func addLabel(obj *unstructured.Unstructured, k, v string) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[k] = v

	obj.SetLabels(labels)
}

func addGCLabel(obj *unstructured.Unstructured) {
	addLabel(obj, TurboGCLabelKey, TurboGCLabelVal)
}

func removeLabel(obj *unstructured.Unstructured, k string) {
	labels := obj.GetLabels()
	if labels == nil {
		// nothing to do
		return
	}
	if _, exists := labels[k]; exists {
		delete(labels, k)
		obj.SetLabels(labels)
	}
}

func removeGCLabel(obj *unstructured.Unstructured) {
	removeLabel(obj, TurboGCLabelKey)
}

func createClonePod(client *kclient.Clientset, pod *api.Pod,
	parent *unstructured.Unstructured, updateQuotaToAllowMoves bool, nodeName string) (*api.Pod, error) {
	npod := &api.Pod{}

	// This can be made configurable if need be in future
	var excludeKeys = []string{
		"pod-template-hash", // used by replicaset to adopt a pod
		"deployment",        // used by a replicationcontroller to adopt pod
		"deploymentconfig",  // used by a replicationcontroller to adopt pod
	}

	copySpec := true // true = case of bare pod
	if parent != nil {
		// copy pod spec, annotations, and labels from the parent
		kind := parent.GetKind()
		parentName := parent.GetName()
		// pod spec from parent
		podSpecUnstructured, found, err := unstructured.NestedFieldCopy(parent.Object, "spec", "template", "spec")
		if err != nil || !found {
			return nil, fmt.Errorf("movePod: error retrieving podSpec from %s %s: %v", kind, parentName, err)
		}

		podSpec := api.PodSpec{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(podSpecUnstructured.(map[string]interface{}), &podSpec); err != nil {
			return nil, fmt.Errorf("movePod: error converting unstructured pod spec to typed pod spec for %s %s: %v", kind, parentName, err)
		}
		// annotations also from parent
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
		// The podspec and annotations are set here before calling copyPodWithoutSelectedLabels below
		// For a bare pod both are set from the original pod
		npod.Spec = podSpec
		npod.Annotations = annotations
		copySpec = false
	}

	copyPodWithoutSelectedLabels(pod, npod, excludeKeys, copySpec)
	// Set podSpec retrieved from parent. This saves from landing into
	// problems of sidecar containers and injection systems.
	npod.Spec.NodeName = nodeName
	npod.Name = genNewPodName(pod)

	// this annotation can be used for future garbage collection if action is interrupted
	util.AddAnnotation(npod, TurboActionAnnotationKey, TurboMoveAnnotationValue)
	// this annotation references the pod that is being moved
	util.AddAnnotation(npod, TurboMovedPodNameKey, pod.Name)
	// this annotation indicates the time at which the pod was moved. This can be used
	// to throttle how often move actions are generated for a given entity.
	util.AddAnnotation(npod, TurboMovedTimestampMillisKey, strconv.Itoa(int(time.Now().UnixMilli())))

	// This label is used for garbage collection if a given action leaks this pod.
	util.AddLabel(npod, TurboGCLabelKey, TurboGCLabelVal)
	// Sanitize the resource requests & limits
	limitRanges, er := client.CoreV1().LimitRanges(pod.GetNamespace()).List(context.TODO(), metav1.ListOptions{})
	if er == nil && len(limitRanges.Items) > 0 {
		util.SanitizeResources(&npod.Spec)
	}

	podClient := client.CoreV1().Pods(pod.Namespace)
	rpod := &api.Pod{}
	err := wait.PollImmediate(DefaultRetrySleepInterval, DefaultRetryTimeout, func() (bool, error) {
		var innerErr error
		rpod, innerErr = podClient.Create(context.TODO(), npod, metav1.CreateOptions{})
		if innerErr != nil {
			// The quota update might not reflect in the admission controller cache immediately
			// which can still fail the new pod creation even after the quota update.
			// We retry for a short while before failing in such cases.
			if updateQuotaToAllowMoves && apierrors.IsForbidden(innerErr) && strings.Contains(innerErr.Error(), "exceeded quota") {
				// Wait only if quota update to allow moves is enabled.
				return false, nil
			}

			return false, innerErr
		}
		return true, nil
	})
	if err != nil {
		glog.Errorf("Failed to create a new pod while waiting for quota: %s/%s, %v", npod.Namespace, npod.Name, err)
		return nil, err
	}

	glog.V(3).Infof("Create a clone pod success: %s/%s", npod.Namespace, npod.Name)
	glog.V(4).Infof("New pod info: %++v", rpod)

	return rpod, nil
}
