package probe

import (
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/kubernetes/pkg/api"
	kubelettypes "k8s.io/kubernetes/pkg/kubelet/types"

	"github.com/golang/glog"
)

const (
	Kind_DaemonSet             string = "DaemonSet"
	Kind_ReplicationController string = "ReplicationController"
	Kind_ReplicaSet            string = "ReplicaSet"
	Kind_Job                   string = "Job"
)

// CPU returned is in KHz; Mem is in Kb
func GetResourceLimits(pod *api.Pod) (cpuCapacity float64, memCapacity float64, err error) {
	if pod == nil {
		return 0, 0, fmt.Errorf("pod passed in is nil.")
	}

	for _, container := range pod.Spec.Containers {
		limits := container.Resources.Limits
		request := container.Resources.Requests

		memCap := limits.Memory().Value()
		if memCap == 0 {
			memCap = request.Memory().Value()
		}
		cpuCap := limits.Cpu().MilliValue()
		memCapacity += float64(memCap) / 1024
		cpuCapacity += float64(cpuCap) / 1000
	}
	return
}

// CPU returned is in KHz; Mem is in Kb
func GetResourceRequest(pod *api.Pod) (cpuRequest float64, memRequest float64, err error) {
	if pod == nil {
		return 0, 0, fmt.Errorf("pod passed in is nil.")
	}

	for _, container := range pod.Spec.Containers {
		request := container.Resources.Requests

		mem := request.Memory().Value()
		cpu := request.Cpu().MilliValue()
		memRequest += float64(mem) / 1024
		cpuRequest += float64(cpu) / 1000
	}
	return
}

const defaultDelimiter string = ":"

// Get the UUID that will be used in Turbonomic server. Here we build the UUID based on pod UID, namespace and name.
// The out UUID is in format "UID:namespace:name"
func GetTurboPodUUID(pod *api.Pod) string {
	uid := string(pod.UID)
	if strings.ContainsAny(uid, defaultDelimiter) {
		glog.Warningf("The pod UID %s contains the default delimitor %s", uid, defaultDelimiter)
		return ""
	}
	namespace := pod.Namespace
	if strings.ContainsAny(namespace, defaultDelimiter) {
		glog.Warningf("The pod namespace %s contains the default delimitor %s", namespace, defaultDelimiter)
		return ""
	}
	name := pod.Name
	if strings.ContainsAny(name, defaultDelimiter) {
		glog.Warningf("The pod name %s contains the default delimitor %s", name, defaultDelimiter)
		return ""
	}
	return uid + defaultDelimiter + namespace + defaultDelimiter + name
}

func BreakdownTurboPodUUID(uuid string) (uid string, namespace string, name string, err error) {
	components := strings.Split(uuid, defaultDelimiter)
	if len(components) != 3 {
		err = fmt.Errorf("Given string %s is not a Turbo Pod UUID.", uuid)
		return
	}
	uid = components[0]
	namespace = components[1]
	name = components[2]
	return
}

// Returns a bool indicates whether the given pod should be monitored.
// Do not monitor pods running on nodes those are not monitored.
// Do not monitor mirror pods or pods created by DaemonSets.
func monitored(pod *api.Pod) bool {
	if _, exist := notMonitoredNodes[pod.Spec.NodeName]; exist {
		return false
	}

	if isMirrorPod(pod) || isPodCreatedBy(pod, Kind_DaemonSet) {
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
		glog.Warningf("Cannot find createdBy reference for Pod %s/%s", pod.Namespace, pod.Name)
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
	glog.V(4).Infof("The kind is %s", kind)
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

// Find the appType (TODO the name is TBD) of the given pod.
// NOTE This function is highly depend on the name of differetn kinds of pod.
// 	If a pod is created by a kubelet, then the name is like name-nodeName
//	If a pod is created by a replication controller, then the name is like name-random
//	if a pod is created by a deployment, then the name is like name-generated-random
func GetAppType(pod *api.Pod) string {
	if isMirrorPod(pod) {
		nodeName := pod.Spec.NodeName
		na := strings.Split(pod.Name, nodeName)
		return na[0][:len(na[0])-1]
	} else if isPodCreatedBy(pod, Kind_DaemonSet) || isPodCreatedBy(pod, Kind_ReplicationController) || isPodCreatedBy(pod, Kind_Job) {
		generatedName := pod.GenerateName
		return generatedName[:len(generatedName)-1]
	} else if isPodCreatedBy(pod, Kind_ReplicaSet) {
		generatedName := pod.GenerateName
		na := strings.Split(generatedName, "-")
		res := ""
		for i := 0; i < len(na)-2; i++ {
			res = res + na[i]
		}
		return res
	}
	return pod.Name
}
