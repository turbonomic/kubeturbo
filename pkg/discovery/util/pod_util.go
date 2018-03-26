package util

import (
	"encoding/json"
	"fmt"

	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	api "k8s.io/client-go/pkg/api/v1"
	kubelettypes "k8s.io/kubernetes/pkg/kubelet/types"

	"github.com/golang/glog"
	"k8s.io/client-go/kubernetes/typed/core/v1"
	"strings"
)

const (
	Kind_DaemonSet             string = "DaemonSet"
	Kind_ReplicationController string = "ReplicationController"
	Kind_ReplicaSet            string = "ReplicaSet"
	Kind_Job                   string = "Job"

	//a flag indicating whether the object should be monitored or not.
	// only value="false" indicating the object should not be monitored by kubeturbo.
	TurboMonitorAnnotation string = "kubeturbo.io/monitored"
)

// check whether a Kubernetes object is monitored or not by its annotation.
// the object can be: Pod, Service, Namespace, or others
func IsMonitoredFromAnnotation(annotations map[string]string) bool {
	if annotations != nil {
		if v, ok := annotations[TurboMonitorAnnotation]; ok {
			if strings.EqualFold(v, "false") {
				return false
			}
		}
	}

	return true
}

// Returns a bool indicates whether the given pod should be monitored.
// Do not monitor mirror pods or pods created by DaemonSets.
// Do not monitor pods which set "TurboMonitorAnnotation" to "false"
func Monitored(pod *api.Pod) bool {
	if isMirrorPod(pod) || isPodCreatedBy(pod, Kind_DaemonSet) {
		return false
	}

	if !IsMonitoredFromAnnotation(pod.GetAnnotations()) {
		return false
	}
	return true
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
	parentKind, err := findParentObjectKind(pod)
	if err != nil {
		glog.Errorf("%++v", err)
	}
	return parentKind == kind
}

// Find the reference object of the parent entity, which created the given pod.
func FindParentReferenceObject(pod *api.Pod) (*api.ObjectReference, error) {

	annotations := pod.Annotations
	if annotations == nil {
		return nil, nil
	}
	createdByRef, exist := annotations["kubernetes.io/created-by"]
	if !exist {
		glog.V(3).Infof("Warning Cannot find createdBy reference for Pod %s/%s", pod.Namespace, pod.Name)
		return nil, nil
	}

	return GetCreatedByRef(createdByRef)
}

// Find the kind of the parent object that creates the pod, from the annotations of pod.
func findParentObjectKind(pod *api.Pod) (string, error) {
	parentObject, err := FindParentReferenceObject(pod)
	if err != nil {
		return "", err
	}
	if parentObject == nil {
		return "", nil
	}
	kind := parentObject.Kind
	glog.V(4).Infof("The kind of parent object of Pod %s/%s is %s", pod.Namespace, pod.Name, kind)
	return kind, nil
}

func GetCreatedByRef(refData string) (*api.ObjectReference, error) {
	var ref api.SerializedReference
	if err := DecodeJSON(&ref, refData); err != nil {
		return nil, fmt.Errorf("Error getting ObjectReference: %s", err)
	}
	objectRef := ref.Reference
	return &objectRef, nil
}

func DecodeJSON(ref interface{}, data string) error {
	body := []byte(data)
	if err := json.Unmarshal(body, ref); err != nil {
		err = fmt.Errorf("unable to unmarshal %q with error: %v", data, err)
		return err
	}
	return nil
}

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
		if podIsReady(pod) {
			readyPods = append(readyPods, pod)
		} else {
			glog.V(4).Infof("Pod %s is not ready", pod.Name)
		}
	}

	return readyPods
}

// PodIsReady checks if a pod is in Ready status.
func podIsReady(pod *api.Pod) bool {
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
	pod, err := podClient.Get(name, meta_v1.GetOptions{})
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
	podList, err := podClient.List(meta_v1.ListOptions{
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

// Parses the pod entity display name to retrieve the namespace and name of the pod.
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
