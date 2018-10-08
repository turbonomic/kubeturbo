package util

import (
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestAddAnnotation(t *testing.T) {
	pod := &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name: "my-pod-1",
			UID:  "my-pod-1-UID",
		},

		Spec: api.PodSpec{},
	}

	kv := make(map[string]string)
	kv["kubeturbo.io/a"] = "a1"
	kv["kubeturbo.io/b"] = "b1"
	kv["kubeturbo.io/c"] = "c1"

	for k, v := range kv {
		AddAnnotation(pod, k, v)
	}

	annotations := pod.Annotations
	if annotations == nil {
		t.Error("Empty annotations.")
		return
	}

	if len(annotations) != len(kv) {
		t.Errorf("Annotation length not equal: %d Vs. %d", len(annotations), len(kv))
	}

	for k, v := range kv {
		pv, ok := annotations[k]
		if !ok {
			t.Errorf("Not found k(%v) in annotations.", k)
		}

		if pv != v {
			t.Errorf("Annotation value corrupted %v Vs. %v.", pv, v)
		}
	}
}

func TestAddAnnotation_Overwrite(t *testing.T) {
	annotations := make(map[string]string)
	key := "kubeturbo.io/a"
	annotations[key] = "a1"

	pod := &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-pod-1",
			UID:         "my-pod-1-UID",
			Annotations: annotations,
		},

		Spec: api.PodSpec{},
	}

	kv := make(map[string]string)
	kv[key] = "a2"
	kv["kubeturbo.io/b"] = "b1"
	kv["kubeturbo.io/c"] = "c1"

	for k, v := range kv {
		AddAnnotation(pod, k, v)
	}

	annotations = pod.Annotations
	if annotations == nil {
		t.Error("Empty annotations.")
		return
	}

	if len(annotations) != len(kv) {
		t.Errorf("Annotation length not equal: %d Vs. %d", len(annotations), len(kv))
	}

	for k, v := range kv {
		pv, ok := annotations[k]
		if !ok {
			t.Errorf("Not found k(%v) in annotations.", k)
		}

		if pv != v {
			t.Errorf("Annotation value corrupted %v Vs. %v.", pv, v)
		}
	}
}

func TestSupportPrivilegePod(t *testing.T) {
	tests := []struct {
		name string
		pod  *api.Pod
		want bool
	}{
		{name: "pod-without-annotations", pod: newPod("pod-1", nil), want: true},
		{name: "pod-with-empty-annotations", pod: newPod("pod-2", make(map[string]string)), want: true},
		{name: "pod-with-scc-empty", pod: podWithSccAnnotations("pod-3", ""), want: true},
		{name: "pod-with-scc-restricted", pod: podWithSccAnnotations("pod-4", "restricted"), want: true},
		{name: "pod-with-scc-anyuid", pod: podWithSccAnnotations("pod-5", "anyuid"), want: false},
		{name: "pod-with-scc-privilege", pod: podWithSccAnnotations("pod-6", "privilege"), want: false},
		{name: "pod-with-scc-foo", pod: podWithSccAnnotations("pod-7", "foo"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SupportPrivilegePod(tt.pod); got != tt.want {
				t.Errorf("SupportPrivilegePod() = %v, want %v", got, tt.want)
			}
		})
	}
}

func podWithSccAnnotations(name, scc string) *api.Pod {
	annotations := make(map[string]string)
	annotations["openshift.io/scc"] = scc

	return newPod(name, annotations)
}

func newPod(name string, annotations map[string]string) *api.Pod {
	return &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},

		Spec: api.PodSpec{},
	}
}
