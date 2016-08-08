package probe

import (
	"encoding/json"
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	kubelettypes "k8s.io/kubernetes/pkg/kubelet/types"

	"github.com/golang/glog"
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
		if cpuCap == 0 {
			cpuCap = request.Cpu().MilliValue()
		}
		memCapacity += float64(memCap) / 1024
		cpuCapacity += float64(cpuCap) / 1000
	}
	return
}

func monitored(pod *api.Pod) bool {
	if isMirrorPod(pod) {
		return false
	}
	kind, err := findParentObjectKind(pod)
	if err != nil {
		glog.Errorf("Cannot determine due to error: %s", err)
		return false
	}
	glog.V(4).Infof("The kind is %s", kind)
	if kind == "DaemonSet" {
		return false
	}
	return true
}

// Determine if a pod is a mirror pod.
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

// Find the kind of the parent object that creates the pod, from the annotations of pod.
func findParentObjectKind(pod *api.Pod) (string, error) {
	annotations := pod.Annotations
	if annotations == nil {
		return "", nil
	}
	createdByRef, exist := annotations["kubernetes.io/created-by"]
	if !exist {
		glog.Warningf("Cannot find createdBy reference for Pod %s/%s", pod.Namespace, pod.Name)
		return "", nil
	}

	parentRef, err := GetCreatedByRef(createdByRef)
	if err != nil {
		return "", err
	}
	return parentRef.Kind, nil
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
