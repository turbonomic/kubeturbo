package probe

import (
	"testing"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/types"
	"k8s.io/kubernetes/pkg/util/uuid"
)

func TestGetTurboPodUUID(t *testing.T) {
	table := []struct {
		uid       types.UID
		namespace string
		name      string

		illegal bool
	}{
		{
			uid:       uuid.NewUUID(),
			namespace: "default",
			name:      "pod-name",
		},
		{
			uid:       types.UID("abcd-e:fg"),
			namespace: "default",
			name:      "pod-name",

			illegal: true,
		},
		{
			uid:       uuid.NewUUID(),
			namespace: "d:efault",
			name:      "pod-name",

			illegal: true,
		},
		{
			uid:       uuid.NewUUID(),
			namespace: "default",
			name:      "pod-n:ame",

			illegal: true,
		},
	}

	for _, item := range table {
		pod := &api.Pod{}
		pod.UID = item.uid
		pod.Namespace = item.namespace
		pod.Name = item.name
		turboPodUUID := GetTurboPodUUID(pod)

		var expectedUUID string
		if item.illegal {
			expectedUUID = ""
		} else {
			expectedUUID = string(item.uid) + ":" + item.namespace + ":" + item.name
		}
		if expectedUUID != turboPodUUID {
			t.Errorf("Expects %s, got %s", expectedUUID, turboPodUUID)
		}
	}
}

func TestBreakdownTurboPodUUID(t *testing.T) {
	table := []struct {
		uid       string
		namespace string
		name      string

		expectsError bool
	}{
		{
			uid: "abc",
			namespace:"default",
			name:"foo",
		},
		{
			namespace:"default",
			name:"foo",

			expectsError:true,
		},
		{
			uid: "abc",
			name:"foo",

			expectsError:true,
		},
		{
			namespace:"default",
			name:"foo",

			expectsError:true,
		},
	}
	for _, item := range table{
		turboUUID := ""
		if item.uid != "" {
			turboUUID += item.uid + ":"
		}
		if item.namespace != "" {
			turboUUID += item.namespace + ":"
		}
		if item.name != "" {
			turboUUID += item.name
		}
		uid, ns, name, err := BreakdownTurboPodUUID(turboUUID)
		if item.expectsError {
			if err == nil {
				t.Error("Expected error, but got no error.")
			}
		} else {
			if err != nil {
				t.Errorf("Unexpected error, %s", err)
			}
			if uid != item.uid {
				t.Errorf("Expects uid %s, got %s", item.uid, uid)
			}
			if ns != item.namespace {
				t.Errorf("Expects namespace %s, got %s", item.namespace, ns)
			}
			if name != item.name {
				t.Errorf("Expects name %s, got %s", item.name, name)
			}
		}
	}
}
