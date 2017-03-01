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

func monitored(pod *api.Pod) bool {
	// Do not monitor mirror pods and pods created by daemonsets.
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

// Find the reference object of the parent entity, which created the gived pod.
func findParentReferenceObject(pod *api.Pod) (*api.ObjectReference, error) {

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
	parentObject, err := findParentReferenceObject(pod)
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
