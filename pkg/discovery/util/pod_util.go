package util

import (
	"encoding/json"
	"fmt"
	"github.com/turbonomic/kubeturbo/pkg/discovery/detectors"

	api "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubelettypes "k8s.io/kubernetes/pkg/kubelet/types"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/typed/core/v1"
	"strings"
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

// check whether a Kubernetes object is controllable or not by its annotation.
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
	return isPodCreatedBy(pod, Kind_DaemonSet) || detectors.IsDaemonDetected(pod.Name, pod.Namespace)
}

// Returns a boolean that indicates whether the given pod should be controllable.
// Do not monitor mirror pods or pods created by DaemonSets.
func Controllable(pod *api.Pod) bool {
	return !isMirrorPod(pod) && !isPodCreatedBy(pod, Kind_DaemonSet) &&
		IsControllableFromAnnotation(pod.GetAnnotations())
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

func GetPod(kubeClient *client.Clientset, namespace, name string) (*api.Pod, error) {
	return kubeClient.CoreV1().Pods(namespace).Get(name, metav1.GetOptions{})
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
				err = fmt.Errorf("failed to decode parent annoation:%v", err)
				glog.Errorf("%v\n%v", err, value)
				return "", "", err
			}

			return ref.Reference.Kind, ref.Reference.Name, nil
		}
	}

	glog.V(4).Infof("no parent-info for pod-%v/%v in Annotations.", pod.Namespace, pod.Name)

	return "", "", nil
}

// get grandParent(parent's parent) information of a pod: kind, name
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
