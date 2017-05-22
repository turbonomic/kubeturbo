package probe

import (
	"k8s.io/kubernetes/pkg/api"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	kubelettypes "k8s.io/kubernetes/pkg/kubelet/types"
	"reflect"
	"testing"
)

func TestIsMirrorPod(t *testing.T) {
	table := []struct {
		annotations map[string]string

		expects bool
	}{
		{
			annotations: map[string]string{
				kubelettypes.ConfigMirrorAnnotationKey: "foo",
			},
			expects: true,
		},
		{
			annotations: map[string]string{
				"foo": "bar",
			},
			expects: false,
		},
		{
			expects: false,
		},
	}
	for _, item := range table {
		pod := &api.Pod{}
		pod.Annotations = item.annotations
		res := isMirrorPod(pod)
		if res != item.expects {
			temp := ""
			if !item.expects {
				temp = "not"
			}
			t.Errorf("pod with annotation %++v is %s a mirror pod, but isMirror returned %t",
				item.annotations, temp, res)
		}
	}
}

func TestBuildPodProperties(t *testing.T) {
	table := []struct {
		podNamespace string
		podName      string
	}{
		{
			podNamespace: "default",
			podName:      "foo",
		},
	}
	for _, item := range table {
		pod := &api.Pod{}
		pod.Namespace = item.podNamespace
		pod.Name = item.podName
		properties := buildPodProperties(pod)

		propertyNamespace := podPropertyNamespace
		podNamespacePropertyName := podPropertyNamePodNamespace
		podNamePropertyName := podPropertyNamePodName
		expectedProperties := []*proto.EntityDTO_EntityProperty{
			{
				Namespace: &propertyNamespace,
				Name:      &podNamespacePropertyName,
				Value:     &item.podNamespace,
			},
			{
				Namespace: &propertyNamespace,
				Name:      &podNamePropertyName,
				Value:     &item.podName,
			},
		}

		if !reflect.DeepEqual(expectedProperties, properties) {
			t.Errorf("Expected: %v, got: %v", expectedProperties, properties)
		}
	}
}

func TestGetPodInfoFromProperty(t *testing.T) {
	table := []struct {
		namespacePropertyNamespace string
		namespacePropertyName      string
		podNamespace               string

		namePropertyNamespace string
		namePropertyName      string
		podName               string

		otherProperties []struct {
			otherPropertyNamespace string
			otherPropertyName      string
			otherPropertyValue     string
		}

		expectedNamespace string
		expectedName      string
	}{
		{
			namespacePropertyNamespace: podPropertyNamespace,
			namespacePropertyName:      podPropertyNamePodNamespace,
			podNamespace:               "default",

			namePropertyNamespace: podPropertyNamespace,
			namePropertyName:      podPropertyNamePodName,
			podName:               "foo",

			expectedNamespace: "default",
			expectedName:      "foo",
		},
		{
			namespacePropertyNamespace: podPropertyNamespace,
			namespacePropertyName:      podPropertyNamePodNamespace,
			podNamespace:               "default",

			namePropertyNamespace: podPropertyNamespace,
			namePropertyName:      podPropertyNamePodName,
			podName:               "foo",

			otherProperties: []struct {
				otherPropertyNamespace string
				otherPropertyName      string
				otherPropertyValue     string
			}{
				{
					"random",
					"bar",
					"foo",
				},
				{
					podPropertyNamespace,
					"bar",
					"foo",
				},
			},

			expectedNamespace: "default",
			expectedName:      "foo",
		},
		{
			namespacePropertyNamespace: "random namespace",
			namespacePropertyName:      podPropertyNamePodNamespace,
			podNamespace:               "default",

			namePropertyNamespace: podPropertyNamespace,
			namePropertyName:      podPropertyNamePodName,
			podName:               "foo",

			expectedNamespace: "",
			expectedName:      "foo",
		},
		{
			namespacePropertyNamespace: podPropertyNamespace,
			namespacePropertyName:      podPropertyNamePodNamespace,
			podNamespace:               "default",

			namePropertyNamespace: "random namespace",
			namePropertyName:      podPropertyNamePodName,
			podName:               "foo",

			expectedNamespace: "default",
			expectedName:      "",
		},
	}
	for i, item := range table {
		properties := []*proto.EntityDTO_EntityProperty{}
		properties = append(properties, &proto.EntityDTO_EntityProperty{
			Namespace: &item.namespacePropertyNamespace,
			Name:      &item.namespacePropertyName,
			Value:     &item.podNamespace,
		})
		properties = append(properties, &proto.EntityDTO_EntityProperty{
			Namespace: &item.namePropertyNamespace,
			Name:      &item.namePropertyName,
			Value:     &item.podName,
		})
		if item.otherProperties != nil {
			for _, otherProperty := range item.otherProperties {
				properties = append(properties, &proto.EntityDTO_EntityProperty{
					Namespace: &otherProperty.otherPropertyNamespace,
					Name:      &otherProperty.otherPropertyName,
					Value:     &otherProperty.otherPropertyValue,
				})
			}
		}

		podNamespace, podName := GetPodInfoFromProperty(properties)
		if podNamespace != item.expectedNamespace {
			t.Errorf("Test case %d failed: exptected namespace %s, got %s",
				i, item.expectedNamespace, podNamespace)
		}
		if podName != item.expectedName {
			t.Errorf("Test case %d failed: expected name %s, got %s", i, item.expectedName, podName)
		}
	}
}
