package util

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/detectors"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
	kubelettypes "k8s.io/kubernetes/pkg/kubelet/types"

	goutil "github.com/turbonomic/kubeturbo/pkg/util"
)

const (
	Kind_DaemonSet             string = "DaemonSet"
	Kind_ReplicationController string = "ReplicationController"
	Kind_ReplicaSet            string = "ReplicaSet"
	Kind_Job                   string = "Job"

	// A flag indicating whether the object should be controllable or not.
	// only value="false" indicating the object should not be controllable by kubeturbo.
	// TODO: [Deprecated] Use TurboControllableAnnotation instead
	TurboMonitorAnnotation      string = "kubeturbo.io/monitored"
	TurboControllableAnnotation string = "kubeturbo.io/controllable"
)

type podEvent struct {
	eType   string
	reason  string
	message string
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
	controllable := !isMirrorPod(pod) && !isPodCreatedBy(pod, Kind_DaemonSet) &&
		IsControllableFromAnnotation(pod.GetAnnotations())
	if !controllable {
		glog.V(4).Infof("Pod %s/%s is not controllable", pod.Namespace, pod.Name)
	}
	return controllable
}

// Check if a pod is a mirror pod.
func isMirrorPod(pod *api.Pod) bool {
	annotations := pod.Annotations
	if annotations == nil {
		return false
	}
	if _, exist := annotations[kubelettypes.ConfigMirrorAnnotationKey]; exist {
		glog.V(4).Infof("Find a mirror pod: %s/%s", pod.Namespace, pod.Name)
		return true
	}
	return false
}

// Check is a pod is created by the given type of entity.
func isPodCreatedBy(pod *api.Pod, kind string) bool {
	parentKind, _, err := GetPodParentInfo(pod)
	if err != nil {
		glog.Errorf("%++v", err)
	}
	return parentKind == kind
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
	glog.Errorf("Unable to get status for pod %s", pod.Name)
	return false
}

// GetPodInPhase finds the pod with the specific name in the specific phase (e.g., Running).
// If pod not found, nil pod pointer will be returned without error.
func GetPodInPhase(podClient v1.PodInterface, name string, phase api.PodPhase) (*api.Pod, error) {
	pod, err := podClient.Get(name, metav1.GetOptions{})
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
	podList, err := podClient.List(metav1.ListOptions{
		FieldSelector: "status.phase=" + string(phase) + ",metadata.uid=" + uid,
	})

	if err != nil {
		return nil, fmt.Errorf("Error to get pod with uid %s: %s", uid, err)
	}

	if podList == nil || len(podList.Items) == 0 {
		err = fmt.Errorf("Pod with uid %s in phase %v not found", uid, phase)
		glog.Error(err.Error())
		return nil, err
	}

	glog.V(3).Infof("Found pod in phase %v by uid %s", phase, uid)
	return &podList.Items[0], nil
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

func parseOwnerReferences(owners []metav1.OwnerReference) (string, string) {
	for i := range owners {
		owner := &owners[i]

		if owner == nil || owner.Controller == nil {
			glog.Warningf("Nil OwnerReference")
			continue
		}

		if *(owner.Controller) && len(owner.Kind) > 0 && len(owner.Name) > 0 {
			return owner.Kind, owner.Name
		}
	}

	return "", ""
}

// GetPodParentInfo gets parent information of a pod
func GetPodParentInfo(pod *api.Pod) (string, string, error) {
	//1. check ownerReferences:

	if pod.OwnerReferences != nil && len(pod.OwnerReferences) > 0 {
		kind, name := parseOwnerReferences(pod.OwnerReferences)
		if len(kind) > 0 && len(name) > 0 {
			return kind, name, nil
		}
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
				return "", "", err
			}

			return ref.Reference.Kind, ref.Reference.Name, nil
		}
	}

	glog.V(4).Infof("no parent-info for pod-%v/%v in Annotations.", pod.Namespace, pod.Name)

	return "", "", nil
}

// GetPodGrandInfo gets grandParent (parent's parent) information of a pod: kind, name
// If parent does not have parent, then return parent info.
// Note: if parent kind is "ReplicaSet", then its parent's parent can be a "Deployment"
func GetPodGrandInfo(kclient *client.Clientset, pod *api.Pod) (string, string, error) {
	//1. get Parent info: kind and name;
	kind, name, err := GetPodParentInfo(pod)
	if err != nil {
		return "", "", err
	}

	//2. if parent is "ReplicaSet", check parent's parent
	if strings.EqualFold(kind, Kind_ReplicaSet) {
		//2.1 get parent object
		rs, err := kclient.ExtensionsV1beta1().ReplicaSets(pod.Namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			err = fmt.Errorf("Failed to get ReplicaSet[%v/%v]: %v", pod.Namespace, name, err)
			glog.Error(err.Error())
			return "", "", err
		}

		//2.2 get parent's parent info by parsing ownerReferences:
		// TODO: The ownerReferences of ReplicaSet is supported only in 1.6.0 and afetr
		if rs.OwnerReferences != nil && len(rs.OwnerReferences) > 0 {
			gkind, gname := parseOwnerReferences(rs.OwnerReferences)
			if len(gkind) > 0 && len(gname) > 0 {
				return gkind, gname, nil
			}
		}
	}

	return kind, name, nil
}

// WaitForPodReady checks the readiness of a given pod with a retry limit and a timeout, whichever
// comes first. If a nodeName is provided, also checks that the hosting node matches that in the
// pod specification
//
// TODO:
// Use k8s watch API to eliminate the need for polling and improve efficiency
func WaitForPodReady(client *client.Clientset, namespace, podName, nodeName string,
	retry int, interval time.Duration) error {
	// check pod readiness with retries
	timeout := time.Duration(retry+1) * interval
	err := goutil.RetrySimple(retry, timeout, interval, func() (bool, error) {
		return checkPodNode(client, namespace, podName, nodeName)
	})
	// log a list of unique events that belong to the pod
	// warning events are logged in Error level, other events are logged in Info level
	podEvents := getPodEvents(client, namespace, podName)
	for _, pe := range podEvents {
		if pe.eType == api.EventTypeWarning {
			glog.Errorf("Pod %s: %s", podName, pe.message)
		} else {
			glog.V(2).Infof("Pod %s: %s", podName, pe.message)
		}
	}
	return err
}

// checkPodNode checks the readiness of a given pod
// The boolean return value indicates if this function needs to be retried
func checkPodNode(kubeClient *client.Clientset, namespace, podName, nodeName string) (bool, error) {
	pod, err := kubeClient.CoreV1().Pods(namespace).Get(podName, metav1.GetOptions{})
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
func getPodEvents(kubeClient *client.Clientset, namespace, name string) (podEvents []podEvent) {
	podEvents = []podEvent{}
	// Get the pod
	pod, err := kubeClient.CoreV1().Pods(namespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return
	}
	// Get events that belong to the given pod
	events, err := kubeClient.CoreV1().Events(namespace).List(metav1.ListOptions{
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
		podEvents = append(podEvents, podEvent{
			eType:   item.Type,
			reason:  item.Reason,
			message: item.Message,
		})
	}
	return
}
