package old

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	api "k8s.io/client-go/pkg/api/v1"
	kubelettypes "k8s.io/kubernetes/pkg/kubelet/types"

	"github.com/golang/glog"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
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
		err = errors.New("pod passed in is nil.")
		return
	}

	for _, container := range pod.Spec.Containers {
		limits := container.Resources.Limits

		memCap := limits.Memory().Value()
		cpuCap := limits.Cpu().MilliValue()
		memCapacity += float64(memCap) / 1024
		cpuCapacity += float64(cpuCap) / 1000
	}
	return
}

// CPU returned is in KHz; Mem is in Kb
func GetResourceRequest(pod *api.Pod) (cpuRequest float64, memRequest float64, err error) {
	if pod == nil {
		err = errors.New("pod passed in is nil.")
		return
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

// Returns a bool indicates whether the given pod should be monitored.
// Do not monitor pods running on nodes those are not monitored.
// Do not monitor mirror pods or pods created by DaemonSets.
func monitored(pod *api.Pod, notMonitoredNodes map[string]struct{}) bool {
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

// Find the appType (TODO the name is TBD) of the given pod.
// NOTE This function is highly depend on the name of different kinds of pod.
// 	If a pod is created by a kubelet, then the name is like name-nodeName
//	If a pod is created by a replication controller, then the name is like name-random
//	if a pod is created by a deployment, then the name is like name-generated-random
func GetAppType(pod *api.Pod) string {
	if isMirrorPod(pod) {
		nodeName := pod.Spec.NodeName
		na := strings.Split(pod.Name, nodeName)
		result := na[0]
		if len(result) > 1 {
			return result[:len(result)-1]
		}
		return result
	} else {
		parent, err := FindParentReferenceObject(pod)
		if err != nil {
			glog.Errorf("fail to getAppType: %v", err.Error())
			return ""
		}

		if parent == nil {
			return pod.Name
		}

		//TODO: if parent.Kind==ReplicaSet:
		//       try to find the Deployment if it has.
		//      or extract the Deployment Name by string operations
		return parent.Name
	}
}

// A pod in-cluster unique ID is namespace/name
func GetPodClusterID(namespace, name string) string {
	return namespace + "/" + name
}

func GetPodClusterIDFromPod(pod *api.Pod) string {
	return GetPodClusterID(pod.Namespace, pod.Name)
}

func BreakdownPodClusterID(podID string) (namespace string, name string, err error) {
	components := strings.Split(podID, "/")
	if len(components) != 2 {
		err = fmt.Errorf("%s is not a pod in-cluster ID.", podID)
	} else {
		namespace = components[0]
		name = components[1]
	}
	return
}

const (
	// TODO currently in the server side only properties in "DEFAULT" namespaces are respected. Ideally we should use "Kubernetes-Pod".
	podPropertyNamespace = "DEFAULT"

	podPropertyNamePodNamespace = "KubernetesPodNamespace"

	podPropertyNamePodName = "KubernetesPodName"
)

// Build entity properties of a pod. The properties are consisted of name and namespace of a pod.
func BuildPodProperties(pod *api.Pod) []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	propertyNamespace := podPropertyNamespace
	podNamespacePropertyName := podPropertyNamePodNamespace
	podNamespacePropertyValue := pod.Namespace
	namespaceProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &podNamespacePropertyName,
		Value:     &podNamespacePropertyValue,
	}
	properties = append(properties, namespaceProperty)

	podNamePropertyName := podPropertyNamePodName
	podNamePropertyValue := pod.Name
	nameProperty := &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &podNamePropertyName,
		Value:     &podNamePropertyValue,
	}
	properties = append(properties, nameProperty)

	return properties
}

// Get the namespace and name of a pod from entity property.
func GetPodInfoFromProperty(properties []*proto.EntityDTO_EntityProperty) (podNamespace string, podName string) {
	if properties == nil {
		return
	}
	for _, property := range properties {
		if property.GetNamespace() != podPropertyNamespace {
			continue
		}
		if podNamespace == "" && property.GetName() == podPropertyNamePodNamespace {
			podNamespace = property.GetValue()
		}
		if podName == "" && property.GetName() == podPropertyNamePodName {
			podName = property.GetValue()
		}
		if podNamespace != "" && podName != "" {
			return
		}
	}
	return
}
