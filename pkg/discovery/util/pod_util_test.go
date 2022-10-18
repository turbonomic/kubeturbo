package util

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/golang/glog"
	"github.com/stretchr/testify/assert"

	k8sapi "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
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

	if len(annotation) == 0 && !IsControllableFromAnnotation(acc.GetAnnotations()) {
		t.Errorf("Object %v with empty annotation should be controllable.", acc.GetName())
	}

	setMonitorFlag(annotation, true)
	if !IsControllableFromAnnotation(acc.GetAnnotations()) {
		t.Errorf("Object %v should be controllable.", acc.GetName())
	}

	setMonitorFlag(annotation, false)
	if IsControllableFromAnnotation(acc.GetAnnotations()) {
		t.Errorf("Object %v should be controllable.", acc.GetName())
	}
}

func TestIsMonitoredFromAnnotation_Pod(t *testing.T) {
	pod := createPod()

	if !IsControllableFromAnnotation(pod.GetAnnotations()) {
		t.Error("Pod without annotation should be controllable.")
	}

	pod.Annotations = make(map[string]string)
	checkObject(pod, t)
}

func TestIsMonitoredFromAnnotation_Service(t *testing.T) {
	svc := createService()

	if !IsControllableFromAnnotation(svc.GetAnnotations()) {
		t.Error("Service without annotation should be controllable")
	}

	svc.Annotations = make(map[string]string)
	checkObject(svc, t)
}

func TestIsMonitoredFromAnnotation_Namespace(t *testing.T) {
	ns := createNamespace()

	if !IsControllableFromAnnotation(ns.GetAnnotations()) {
		t.Error("Service without annotation should be controllable")
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

	pod.ObjectMeta.Annotations[k8sapi.MirrorPodAnnotationKey] = "yes"
	if Controllable(pod) {
		t.Error("Mirrored pod should not be controllable")
	}

	podInDaemonSet := makePodInDaemonSet()
	if !Controllable(podInDaemonSet) {
		t.Errorf("Pod in daemon set must be controllable")
	}

	// Here is a different way to specify that a pod was created via a DaemonSet.
	podInDaemonSet = newPod("pod-3")
	podInDaemonSet.ObjectMeta.Annotations = make(map[string]string)

	var ref k8sapi.SerializedReference
	ref.Reference.Kind = "DaemonSet"
	ref.Reference.Name = "yes"
	refbytes, _ := json.Marshal(&ref)
	podInDaemonSet.ObjectMeta.Annotations["kubernetes.io/created-by"] = string(refbytes)
	if !Controllable(podInDaemonSet) {
		t.Errorf("Pod in daemon set must be controllable")
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

func TestGetPodInPhaseByUid(t *testing.T) {
	podInterface := &MockPodInterface{}

	// An existing pod should return no error
	pod, err := GetPodInPhaseByUid(podInterface, testPodUID1, k8sapi.PodRunning)
	if err != nil {
		t.Errorf("Failed GetPodInPhaseByUid: %v,", err)
	}
	// Found pod should match the UID
	assert.Equal(t, string(pod.UID), testPodUID1)

	// A non existing pod should return an error
	_, err = GetPodInPhaseByUid(podInterface, testPodUID3, k8sapi.PodRunning)
	if err == nil {
		t.Errorf("GetPodInPhaseByUid should have returned an error.")
	}
}

func TestGetMirrorPodPrefix(t *testing.T) {
	pods := getPodsWithMirrorPods()

	prefix1, res1 := GetMirrorPodPrefix(pods[0])
	assert.Equal(t, "pod1-prefix-", prefix1)
	assert.Equal(t, true, res1)
	pref2, res2 := GetMirrorPodPrefix(pods[1])
	assert.Equal(t, "", pref2)
	assert.Equal(t, false, res2)
}

func TestGetMirrorPods(t *testing.T) {
	pods := getPodsWithMirrorPods()

	mirrorPods := GetMirrorPods(pods)
	assert.Equal(t, 1, len(mirrorPods))
	assert.Equal(t, pods[0], mirrorPods[0])
}

func TestGetMirrorPodPrefixToNodeNames(t *testing.T) {
	pods := getPodsWithMirrorPods()

	prefixToNodeNames := GetMirrorPodPrefixToNodeNames(pods)
	assert.Equal(t, 1, len(prefixToNodeNames))
	assert.Equal(t, sets.NewString("node"), prefixToNodeNames["pod1-prefix-"])

}

func getPodsWithMirrorPods() []*v1.Pod {
	return []*v1.Pod{
		// mirror pod
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod1-prefix-node",
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: Kind_Node,
						Name: "node",
					},
				},
			},
			Spec: k8sapi.PodSpec{
				NodeName: "node",
			},
		},
		// not mirror pod
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod2",
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "controller",
						Name: "node",
					},
				},
			},
			Spec: k8sapi.PodSpec{
				NodeName: "node",
			},
		},
	}
}
