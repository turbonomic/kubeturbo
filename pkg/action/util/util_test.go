package util

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	api "k8s.io/client-go/pkg/api/v1"
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
