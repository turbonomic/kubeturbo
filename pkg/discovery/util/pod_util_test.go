package util

import (
	k8sapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kubelettypes "k8s.io/kubernetes/pkg/kubelet/types"

	"encoding/json"
	"fmt"
	"github.com/golang/glog"
	"reflect"
	"testing"
)

func createPod() *k8sapi.Pod {
	pod := &k8sapi.Pod{}
	pod.Kind = "Pod"
	pod.APIVersion = "v1"
	pod.Name = "my-pod-1"
	pod.UID = "my-pod-1-UID"

	return pod
}

func createService() *k8sapi.Service {
	svc := &k8sapi.Service{}

	svc.Kind = "Service"
	svc.APIVersion = "v1"
	svc.Name = "my-web-service"
	svc.UID = "my-web-service-UID"

	return svc
}

func createNamespace() *k8sapi.Namespace {
	ns := &k8sapi.Namespace{}

	ns.Kind = "Namespace"
	ns.APIVersion = "v1"
	ns.Name = "my-namespace"
	ns.UID = "my-namespace-UID"
	return ns
}

func setMonitorFlag(annotations map[string]string, flag bool) error {
	if annotations == nil {
		glog.Error("annotation is nil")
		return fmt.Errorf("Annotation is nil")
	}

	if flag {
		annotations[TurboMonitorAnnotation] = "true"
	} else {
		annotations[TurboMonitorAnnotation] = "false"
	}

	return nil
}

func checkObject(obj interface{}, t *testing.T) {
	acc, err := meta.Accessor(obj)
	if err != nil {
		glog.Errorf("Not a Kubernetes Object: %v", err)
		return
	}

	annotation := acc.GetAnnotations()
	if annotation == nil {
		glog.Errorf("annotation is nil")
		return
	}

	if len(annotation) == 0 && !IsMonitoredFromAnnotation(acc.GetAnnotations()) {
		t.Errorf("Object %v with empty annotation should be monitored.", acc.GetName())
	}

	setMonitorFlag(annotation, true)
	if !IsMonitoredFromAnnotation(acc.GetAnnotations()) {
		t.Errorf("Object %v should be monitored.", acc.GetName())
	}

	setMonitorFlag(annotation, false)
	if IsMonitoredFromAnnotation(acc.GetAnnotations()) {
		t.Errorf("Object %v should be monitored.", acc.GetName())
	}
}

func TestIsMonitoredFromAnnotation_Pod(t *testing.T) {
	pod := createPod()

	if !IsMonitoredFromAnnotation(pod.GetAnnotations()) {
		t.Error("Pod without annotation should be monitored.")
	}

	pod.Annotations = make(map[string]string)
	checkObject(pod, t)
}

func TestIsMonitoredFromAnnotation_Service(t *testing.T) {
	svc := createService()

	if !IsMonitoredFromAnnotation(svc.GetAnnotations()) {
		t.Error("Service without annotation should be monitored")
	}

	svc.Annotations = make(map[string]string)
	checkObject(svc, t)
}

func TestIsMonitoredFromAnnotation_Namespace(t *testing.T) {
	ns := createNamespace()

	if !IsMonitoredFromAnnotation(ns.GetAnnotations()) {
		t.Error("Service without annotation should be monitored")
	}

	ns.Annotations = make(map[string]string)
	checkObject(ns, t)
}

func TestGetReadyPods(t *testing.T) {
	// Set up different PodCondition objects
	pcReadyTrue := k8sapi.PodCondition{Type: k8sapi.PodReady, Status: k8sapi.ConditionTrue}
	pcReadyFalse := k8sapi.PodCondition{Type: k8sapi.PodReady, Status: k8sapi.ConditionFalse}
	pcReadyUnknown := k8sapi.PodCondition{Type: k8sapi.PodReady, Status: k8sapi.ConditionUnknown}
	pcOther := k8sapi.PodCondition{Type: "foo", Status: "bar"}

	podReady1 := newPod("pod-1", pcReadyTrue)
	podReady2 := newPod("pod-2", pcReadyTrue, pcOther)
	podNotReady := newPod("pod-3", pcReadyFalse, pcOther)
	podUnknown := newPod("pod-4", pcReadyUnknown, pcOther)
	podNoCond := newPod("pod-5", pcOther)

	tests := []struct {
		name string
		pods []*k8sapi.Pod
		want []*k8sapi.Pod
	}{
		{
			name: "pods-nil",
			pods: nil,
			want: []*k8sapi.Pod{},
		},
		{
			name: "pods-empty",
			pods: []*k8sapi.Pod{},
			want: []*k8sapi.Pod{},
		},
		{
			name: "pod-ready",
			pods: []*k8sapi.Pod{podReady1},
			want: []*k8sapi.Pod{podReady1},
		},
		{
			name: "pods-ready",
			pods: []*k8sapi.Pod{podReady1, podReady2},
			want: []*k8sapi.Pod{podReady1, podReady2},
		},
		{
			name: "pod-not-ready",
			pods: []*k8sapi.Pod{podNotReady},
			want: []*k8sapi.Pod{},
		},
		{
			name: "pods-not-ready",
			pods: []*k8sapi.Pod{podNotReady, podUnknown},
			want: []*k8sapi.Pod{},
		},
		{
			name: "pod-no-cond",
			pods: []*k8sapi.Pod{podNoCond},
			want: []*k8sapi.Pod{},
		},
		{
			name: "pods-ready-and-no-cond",
			pods: []*k8sapi.Pod{podNoCond, podReady1},
			want: []*k8sapi.Pod{podReady1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetReadyPods(tt.pods); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Test %s: GetReadyPods() = %++v, want %++v", tt.name, got, tt.want)
			}
		})
	}
}

func TestMonitored_DaemonSet(t *testing.T) {
	// With annotation
	pod := newPod("pod-foo")
	pod.Annotations = map[string]string{
		"kubernetes.io/created-by": `{"reference":{"kind":"DaemonSet","name":"daemon-set-foo"}}`,
	}

	if !Monitored(pod) {
		t.Errorf("The pod in DaemonSet must be monitored")
	}

	// With Owner reference
	podWithOwnerRef := makePodInDaemonSet()
	if !Monitored(podWithOwnerRef) {
		t.Errorf("The pod (with owner reference) in DaemonSet must be monitored")
	}
}

func makePodInDaemonSet() *k8sapi.Pod {
	podWithOwnerRef := newPod("pod-bar")
	isController := true
	podWithOwnerRef.OwnerReferences = []metav1.OwnerReference{{Kind: "DaemonSet", Name: "daemon-set-foo", Controller: &isController}}
	return podWithOwnerRef
}

func TestMirroredPod(t *testing.T) {
	pod := newPod("pod-1")
	if !Controllable(pod) {
		t.Error("Pod is not controllable and it should be by default")
	}
	// Set an annotation on the pod to indicate that it is mirrored
	pod.ObjectMeta.Annotations = make(map[string]string)
	pod.ObjectMeta.Annotations["some-random-key"] = "foo"
	if !Controllable(pod) {
		t.Error("Non-mirrored pod should be controllable")
	}

	pod.ObjectMeta.Annotations[kubelettypes.ConfigMirrorAnnotationKey] = "yes"
	if Controllable(pod) {
		t.Error("Mirrored pod should not be controllable")
	}

	podInDaemonSet := makePodInDaemonSet()
	if Controllable(podInDaemonSet) {
		t.Errorf("Pod in daemon set cannot be controllable")
	}

	// Here is a different way to specify that a pod was created via a DaemonSet.
	podInDaemonSet = newPod("pod-3")
	podInDaemonSet.ObjectMeta.Annotations = make(map[string]string)

	var ref k8sapi.SerializedReference
	ref.Reference.Kind = "DaemonSet"
	ref.Reference.Name = "yes"
	refbytes, _ := json.Marshal(&ref)
	podInDaemonSet.ObjectMeta.Annotations["kubernetes.io/created-by"] = string(refbytes)
	if Controllable(podInDaemonSet) {
		t.Errorf("Pod in daemon set cannot be controllable")
	}

}

func newPod(name string, podConds ...k8sapi.PodCondition) *k8sapi.Pod {
	return &k8sapi.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},

		Spec: k8sapi.PodSpec{},

		Status: k8sapi.PodStatus{
			Conditions: podConds,
		},
	}
}
