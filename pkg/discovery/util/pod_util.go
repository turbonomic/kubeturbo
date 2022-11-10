package util

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	client "k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/detectors"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	commonutil "github.com/turbonomic/kubeturbo/pkg/util"
)

const (
	Kind_DaemonSet             string = "DaemonSet"
	Kind_ReplicationController string = "ReplicationController"
	Kind_ReplicaSet            string = "ReplicaSet"
	Kind_Job                   string = "Job"
	// Node owner reference kind is injected by Kubelet if Pod is mirror Pod starting from K8s v1.17
	// https://github.com/kubernetes/enhancements/blob/master/keps/sig-auth/20190916-noderestriction-pods.md#ownerreferences
	Kind_Node string = "Node"

	// A flag indicating whether the object should be controllable or not.
	// only value="false" indicating the object should not be controllable by kubeturbo.
	// TODO: [Deprecated] Use TurboControllableAnnotation instead
	TurboMonitorAnnotation      string = "kubeturbo.io/monitored"
	TurboControllableAnnotation string = "kubeturbo.io/controllable"
)

type PodEvent struct {
	EType   string
	Reason  string
	Message string
}

// IsControllableFromAnnotation checks whether a Kubernetes object is controllable or not by its annotation.
// The annotation can be either "kubeturbo.io/controllable" or "kubeturbo.io/monitored".
// the object can be: Pod, Service, Namespace, or others.  If no annotation
// exists, the default value is true.
func IsControllableFromAnnotation(annotations map[string]string) bool {
	if annotations != nil {
		anno1 := annotations[TurboMonitorAnnotation]
		anno2 := annotations[TurboControllableAnnotation]
		if strings.EqualFold(anno1, "false") || strings.EqualFold(anno2, "false") {
			return false
		}
	}

	return true
}

// Returns a boolean that indicates whether the given pod is a daemon pod.  A daemon pod
// is not suspendable, clonable, or movable, and is not considered when counting
// customers of a supplier when checking whether the supplier can suspend.
func Daemon(pod *api.Pod) bool {
	isDaemon := isPodCreatedBy(pod, Kind_DaemonSet) || detectors.IsDaemonDetected(pod.Name, pod.Namespace)
	if isDaemon {
		glog.V(4).Infof("Pod %s/%s is a daemon", pod.Namespace, pod.Name)
	}
	return isDaemon
}

// Returns a boolean that indicates whether the given pod should be controllable.
// Do not monitor mirror pods or pods created by DaemonSets.
func Controllable(pod *api.Pod) bool {
	controllable := !isMirrorPod(pod) && IsControllableFromAnnotation(pod.GetAnnotations())
	if !controllable {
		glog.V(4).Infof("Pod %s/%s is not controllable", pod.Namespace, pod.Name)
	}
	return controllable
}

// hasController checks if a pod is deployed by K8s controller
func HasController(pod *api.Pod) bool {
	if pod.OwnerReferences == nil {
		return false
	}
	return !hasNodeOwner(pod)
}

// extracts mirror pod prefix. Returns the prefix and extraction result.
func GetMirrorPodPrefix(pod *api.Pod) (string, bool) {
	if !isMirrorPod(pod) {
		return "", false
	}
	return strings.Replace(pod.Name, pod.Spec.NodeName, "", 1), true
}

func GetMirrorPods(pods []*api.Pod) []*api.Pod {
	glog.V(3).Info("Getting mirror pods.")
	mirrorPods := []*api.Pod{}
	for _, pod := range pods {
		if isMirrorPod(pod) {
			mirrorPods = append(mirrorPods, pod)
		}
	}
	glog.V(3).Infof("Found %+v mirror pods", len(mirrorPods))
	glog.V(4).Info(mirrorPods)
	return mirrorPods
}

func GetMirrorPodPrefixToNodeNames(pods []*api.Pod) map[string]sets.String {
	glog.V(3).Info("maping mirror pod prefixes to node names.")
	prefixToNodeNames := make(map[string]sets.String)
	for _, pod := range pods {
		prefix, ok := GetMirrorPodPrefix(pod)
		if !ok {
			// Not a mirror pod
			continue
		}
		nodeNames, found := prefixToNodeNames[prefix]
		if !found {
			prefixToNodeNames[prefix] = sets.NewString(pod.Spec.NodeName)
		} else {
			nodeNames.Insert(pod.Spec.NodeName)
		}
	}

	glog.V(3).Infof("Found %+v static pod prefix keys.", len(prefixToNodeNames))
	glog.V(4).Info(prefixToNodeNames)
	return prefixToNodeNames
}

// Check if a pod is a mirror pod.
func isMirrorPod(pod *api.Pod) bool {
	annotations := pod.Annotations
	if annotations != nil {
		if _, exist := annotations[api.MirrorPodAnnotationKey]; exist {
			glog.V(4).Infof("Found a mirror pod: %s/%s", pod.Namespace, pod.Name)
			return true
		}
	}

	return hasNodeOwner(pod)
}

// hasNodeOwner checks if a pod has a single owner reference with kind as Node. Node owner reference is injected by Kubelet
// to identify if a pod is mirror pod starting in K8s v1.17:
// https://github.com/kubernetes/enhancements/blob/master/keps/sig-auth/20190916-noderestriction-pods.md#ownerreferences
func hasNodeOwner(pod *api.Pod) bool {
	if pod.OwnerReferences != nil && len(pod.OwnerReferences) == 1 && pod.OwnerReferences[0].Kind == Kind_Node {
		glog.V(4).Infof("Found a mirror pod: %s/%s", pod.Namespace, pod.Name)
		return true
	}
	return false
}

// Check is a pod is created by the given type of entity.
func isPodCreatedBy(pod *api.Pod, kind string) bool {
	ownerInfo, err := GetPodParentInfo(pod)
	if err != nil {
		glog.Errorf("%++v", err)
	}
	return ownerInfo.Kind == kind
}

// GroupPodsByNode groups all pods based on their hosting node
func GroupPodsByNode(pods []*api.Pod) map[string][]*api.Pod {
	podsNodeMap := make(map[string][]*api.Pod)
	if pods == nil {
		glog.Error("Pod list is nil")
		return podsNodeMap
	}
	for _, pod := range pods {
		nodeName := pod.Spec.NodeName
		podList, exist := podsNodeMap[nodeName]
		if !exist {
			podList = []*api.Pod{}
		}
		podList = append(podList, pod)
		podsNodeMap[nodeName] = podList
	}
	return podsNodeMap
}

// GetReadyPods returns pods that in Ready status
func GetReadyPods(pods []*api.Pod) []*api.Pod {
	readyPods := []*api.Pod{}

	for _, pod := range pods {
		if PodIsReady(pod) {
			readyPods = append(readyPods, pod)
		} else {
			glog.V(4).Infof("Pod %s is not ready", pod.Name)
		}
	}

	return readyPods
}

// PodIsReady checks if a pod is in Ready status.
func PodIsReady(pod *api.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == api.PodReady {
			return condition.Status == api.ConditionTrue
		}
	}
	//glog.Errorf("Unable to get status for pod %s", pod.Name)
	return false
}

// PodIsPending checks if a scheduled pod is in Pending status
func PodIsPending(pod *api.Pod) bool {
	if pod.Status.Phase != api.PodPending {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == api.PodScheduled {
			return condition.Status == api.ConditionTrue
		}
	}
	return false
}

// GetPodInPhase finds the pod with the specific name in the specific phase (e.g., Running).
// If pod not found, nil pod pointer will be returned without error.
func GetPodInPhase(podClient v1.PodInterface, name string, phase api.PodPhase) (*api.Pod, error) {
	pod, err := podClient.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Error while getting pod %s in phase %v: %v", name, phase, err.Error())
		return nil, err
	}

	if pod == nil {
		err = fmt.Errorf("Pod %s not found", name)
		glog.Error(err.Error())
		return nil, err
	}

	if pod.Status.Phase != phase {
		return nil, fmt.Errorf("Pod %s is not in phase %v", name, phase)
	}

	return pod, nil
}

// GetPodInPhaseByUid finds the pod with the specific uid in the specific phase (e.g., Running).
// If pod not found, nil pod pointer will be returned without error.
func GetPodInPhaseByUid(podClient v1.PodInterface, uid string, phase api.PodPhase) (*api.Pod, error) {
	podList, err := podClient.List(context.TODO(), metav1.ListOptions{
		FieldSelector: "status.phase=" + string(phase),
	})
	if err != nil {
		return nil, fmt.Errorf("error getting podlist %s", err)
	}

	err = fmt.Errorf("Pod with uid %s in phase %v not found", uid, phase)
	if podList == nil || len(podList.Items) == 0 {
		glog.Error(err.Error())
		return nil, err
	}

	for _, pod := range podList.Items {
		if string(pod.UID) == uid {
			glog.V(3).Infof("Found pod in phase %v by uid %s", phase, uid)
			return &pod, nil
		}
	}

	glog.Error(err.Error())
	return nil, err
}

// ParsePodDisplayName parses the pod entity display name to retrieve the namespace and name of the pod.
func ParsePodDisplayName(displayName string) (string, string, error) {
	sep := "/"
	items := strings.Split(displayName, sep)
	if len(items) < 2 {
		err := fmt.Errorf("Cannot get namespace/podname from %v", displayName)
		glog.Error(err.Error())
		return "", "", err
	}

	//1. get Namespace
	namespace := strings.TrimSpace(items[0])
	if len(namespace) < 1 {
		err := fmt.Errorf("Parsed namespace is empty from %v", displayName)
		glog.Errorf(err.Error())
		return "", "", err
	}

	//2. get PodName
	name := strings.TrimSpace(items[1])
	if len(name) < 1 {
		err := fmt.Errorf("Parsed name is empty from %v", displayName)
		glog.Errorf(err.Error())
		return "", "", err
	}

	return namespace, name, nil
}

// GetPodParentInfo gets parent information of a pod: kind, name, uid.
// The result OwnerInfo can be empty, indicating that the pod does not have an owner.
func GetPodParentInfo(pod *api.Pod) (OwnerInfo, error) {
	//1. check ownerReferences:
	ownerInfo, ownerSet := GetOwnerInfo(pod.OwnerReferences)
	if ownerSet {
		return ownerInfo, nil
	}

	glog.V(4).Infof("no parent-info for pod-%v/%v in OwnerReferences.", pod.Namespace, pod.Name)

	//2. check annotations:
	if pod.Annotations != nil && len(pod.Annotations) > 0 {
		key := "kubernetes.io/created-by"
		if value, ok := pod.Annotations[key]; ok {

			var ref api.SerializedReference

			if err := json.Unmarshal([]byte(value), &ref); err != nil {
				err = fmt.Errorf("failed to decode parent annoation: %v", err)
				glog.Errorf("%v\n%v", err, value)
				return OwnerInfo{}, err
			}

			return OwnerInfo{ref.Reference.Kind, ref.Reference.Name, string(ref.Reference.UID), nil}, nil
		}
	}

	glog.V(4).Infof("no parent-info for pod-%v/%v in Annotations.", pod.Namespace, pod.Name)

	return OwnerInfo{}, nil
}

// GetControllerUID get controller UID from the given pod and metrics sink.
func GetControllerUID(pod *api.Pod, metricsSink *metrics.EntityMetricSink) (string, error) {
	podKey := PodKeyFunc(pod)
	ownerUIDMetricId := metrics.GenerateEntityStateMetricUID(metrics.PodType, podKey, metrics.OwnerUID)
	ownerUIDMetric, err := metricsSink.GetMetric(ownerUIDMetricId)
	if err != nil {
		return "", err
	}
	ownerUID := ownerUIDMetric.GetValue()
	controllerUID, ok := ownerUID.(string)
	if !ok {
		return "", fmt.Errorf("owner UID %v is not a string", ownerUID)
	}
	return controllerUID, nil
}

// WaitForPodReady checks the readiness of a given pod with a retry limit and a timeout, whichever
// comes first. If a nodeName is provided, also checks that the hosting node matches that in the
// pod specification
//
// TODO:
// Use k8s watch API to eliminate the need for polling and improve efficiency
func WaitForPodReady(client *client.Clientset, namespace, podName, nodeName string, initDelay time.Duration,
	retry int32, interval time.Duration) error {
	if initDelay > 0 {
		time.Sleep(initDelay)
	}
	// check pod readiness with retries
	timeout := time.Duration(retry+1) * interval
	err := commonutil.RetrySimple(retry, timeout, interval, func() (bool, error) {
		return checkPodNode(client, namespace, podName, nodeName)
	})
	// log a list of unique events that belong to the pod
	// warning events are logged in Error level, other events are logged in Info level
	podEvents := GetPodEvents(client, namespace, podName)
	for _, pe := range podEvents {
		if pe.EType == api.EventTypeWarning {
			glog.Errorf("Pod %s: %s", podName, pe.Message)
		} else {
			glog.V(2).Infof("Pod %s: %s", podName, pe.Message)
		}
	}
	return err
}

// checkPodNode checks the readiness of a given pod
// The boolean return value indicates if this function needs to be retried
func checkPodNode(kubeClient *client.Clientset, namespace, podName, nodeName string) (bool, error) {
	pod, err := kubeClient.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
	if err != nil {
		return true, err
	}
	if pod.Status.Phase == api.PodRunning && PodIsReady(pod) {
		if len(nodeName) > 0 {
			if !strings.EqualFold(pod.Spec.NodeName, nodeName) {
				return false, fmt.Errorf("pod %s is running on an unexpected node %v", podName, pod.Spec.NodeName)
			}
		}
		return false, nil
	}
	warnings := getPodWarnings(pod, podName)
	return true, fmt.Errorf("%s", strings.Join(warnings, ", "))
}

// getPodWarnings gets a list of short warning messages that belong to the given pod
// These messages will be concatenated and sent to the UI
// The warning messages are logged in the following order:
//   - Pod states and reasons
//   - Pod conditions and reasons (false conditions only)
//   - Container states and reasons (abnormal states only)
//
// https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-and-container-status
func getPodWarnings(pod *api.Pod, podName string) (warnings []string) {
	warnings = []string{}
	// pod statuses
	podState := ""
	if pod.DeletionTimestamp != nil {
		podState = fmt.Sprintf("pod %s is being deleted", podName)
	} else {
		podState = fmt.Sprintf("pod %s is in %s state", podName, pod.Status.Phase)
	}
	if pod.Status.Reason != "" {
		podState = podState + " due to " + pod.Status.Reason
	}
	warnings = append(warnings, podState)
	// pod conditions
	visited := make(map[string]bool, 0)
	for _, condition := range pod.Status.Conditions {
		// skip true conditions
		if condition.Status == api.ConditionTrue ||
			visited[condition.Reason] {
			continue
		}
		visited[condition.Reason] = true
		warnings = append(warnings, condition.Reason)
	}
	// container statuses
	for _, containerStatus := range pod.Status.ContainerStatuses {
		name := containerStatus.Name
		state := containerStatus.State
		if state.Waiting != nil {
			warnings = append(warnings,
				fmt.Sprintf("container %s is waiting due to %s",
					name, state.Waiting.Reason))
		}
		if state.Terminated != nil {
			warnings = append(warnings,
				fmt.Sprintf("container %s has terminated due to %s",
					name, state.Terminated.Reason))
		}
	}
	return
}

// getPodEvents gets a list of unique events that belong to the given pod
// These events can be very long, and will only be written to the log file
func GetPodEvents(kubeClient *client.Clientset, namespace, name string) (podEvents []PodEvent) {
	podEvents = []PodEvent{}
	// Get the pod
	pod, err := kubeClient.CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return
	}
	// Get events that belong to the given pod
	events, err := kubeClient.CoreV1().Events(namespace).List(context.TODO(), metav1.ListOptions{
		FieldSelector: "involvedObject.uid=" + string(pod.UID),
	})
	if err != nil {
		return
	}
	// Remove duplicates and create podEvents
	visited := make(map[string]bool, 0)
	for _, item := range events.Items {
		if visited[item.Reason] {
			continue
		}
		visited[item.Reason] = true
		podEvents = append(podEvents, PodEvent{
			EType:   item.Type,
			Reason:  item.Reason,
			Message: item.Message,
		})
	}
	return
}

func GetContainerNames(parent *unstructured.Unstructured) (sets.String, error) {
	podSpecUnstructured, found, err := unstructured.NestedFieldCopy(parent.Object, "spec", "template", "spec")
	if err != nil || !found {
		return nil, fmt.Errorf("error retrieving podSpec from %s %s: %v", parent.GetKind(), parent.GetName(), err)
	}

	podSpec := api.PodSpec{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(podSpecUnstructured.(map[string]interface{}), &podSpec); err != nil {
		return nil, fmt.Errorf("error converting unstructured pod spec to typed pod spec for %s %s: %v", parent.GetKind(), parent.GetName(), err)
	}

	names := []string{}
	for _, container := range podSpec.Containers {
		names = append(names, container.Name)
	}
	return sets.NewString(names...), nil
}
