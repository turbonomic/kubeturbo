package firstclass

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// define inference service struct locally for testing only and to avoid huge amount of packages
// need to check and follow official implementation
type Service struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ServiceSpec `json:"spec,omitempty"`
	// +optional
	Status ServiceStatus `json:"status,omitempty"`
}

type ServiceStatus struct {
}

type ServiceSpec struct {
	// ServiceSpec inlines an unrestricted ConfigurationSpec.
	ConfigurationSpec `json:",inline"`
}

type ConfigurationSpec struct {
	// Template holds the latest specification for the Revision to be stamped out.
	// +optional
	Template RevisionTemplateSpec `json:"template"`
}

type RevisionTemplateSpec struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
}

var (
	_testKNativeRevisionName_      = "revision-test"
	_testKNativeConfigurationName_ = "config-test"
	_testKNativeServiceName_       = "ksvc-test"
)

/*
 	CRDs used in this test are downloaded from official kserve repo
	curl -o configuration.yaml https://raw.githubusercontent.com/knative/serving/refs/heads/main/config/core/300-resources/configuration.yaml
	curl -o revision.yaml https://raw.githubusercontent.com/knative/serving/refs/heads/main/config/core/300-resources/revision.yaml
	curl -o service.yaml https://raw.githubusercontent.com/knative/serving/refs/heads/main/config/core/300-resources/service.yaml
*/

func TestKNativeReplace(t *testing.T) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(".", "test_crds", "knative"),
		},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	assert.NotNil(t, cfg)
	client := dynamic.NewForConfigOrDie(cfg)
	assert.NotNil(t, client)

	prepareNodesForTesting(t, client)

	deploy, deployref, err := prepareKNativeResourcesForTest(t, client)
	assert.ErrorIs(t, nil, err)
	assert.NotNil(t, deploy)

	var replicas int32 = 3
	repStr := fmt.Sprintf("%d", replicas)
	deploy.Spec.Replicas = &replicas

	obj := &unstructured.Unstructured{}
	obj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&deploy)

	kno := &KNativeServiceOwner{Interface: client}
	trueRef := kno.getTrueOwnerReference(deployref, obj)
	assert.NotNil(t, trueRef)

	objMap, err := kno.replace(*trueRef, obj, []string{deploymentReplicasPath})
	assert.ErrorIs(t, nil, err)

	assert.Equal(t, len(objMap), 1)
	for o, paths := range objMap {
		assert.Equal(t, o.GetName(), trueRef.Name)
		assert.Equal(t, len(paths), 1)
		assert.Equal(t, knativeSpecTemplateMetadataAnnotationsPath, paths[0])

		ksvc := &Service{}

		err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, ksvc)
		assert.ErrorIs(t, nil, err)
		assert.Equal(t, ksvc.Spec.Template.Annotations[knativeMinScaleAnnotationKey], repStr)
		assert.Equal(t, ksvc.Spec.Template.Annotations[knativeMaxScaleAnnotationKey], repStr)
	}

	err = testEnv.Stop()
	assert.ErrorIs(t, nil, err)
}

// Test the chain of owners Deployment -> Revision -> Configuration -> KService
func TestKNativeGetTrueOwnerReference(t *testing.T) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(".", "test_crds", "knative"),
		},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	assert.NotNil(t, cfg)
	client := dynamic.NewForConfigOrDie(cfg)
	assert.NotNil(t, client)

	deploy, ref, err := prepareKNativeResourcesForTest(t, client)
	assert.ErrorIs(t, nil, err)
	assert.NotNil(t, deploy)

	var replicas int32 = 1
	deploy.Spec.Replicas = &replicas

	obj := &unstructured.Unstructured{}
	obj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&deploy)
	assert.ErrorIs(t, nil, err)

	kno := &KNativeServiceOwner{Interface: client}

	refService := metav1.OwnerReference{}
	refService.APIVersion, refService.Kind = knativeServiceGVK.ToAPIVersionAndKind()
	refService.Name = _testKNativeServiceName_
	refOut := kno.getTrueOwnerReference(ref, obj)

	assert.Equal(t, refService.APIVersion, refOut.APIVersion)
	assert.Equal(t, refService.Kind, refOut.Kind)
	assert.Equal(t, refService.Name, refOut.Name)

	err = testEnv.Stop()
	assert.ErrorIs(t, nil, err)
}

func TestScaleToZeroWithKNativeService(t *testing.T) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(".", "test_crds", "knative"),
		},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	assert.NotNil(t, cfg)
	client := dynamic.NewForConfigOrDie(cfg)
	assert.NotNil(t, client)

	prepareNodesForTesting(t, client)

	deploy, deployref, err := prepareKNativeResourcesForTest(t, client)
	assert.ErrorIs(t, nil, err)
	assert.NotNil(t, deploy)

	var replicas int32 = 1
	deploy.Spec.Replicas = &replicas
	deploy.Annotations = map[string]string{
		knativeScaleToZeroAnnotationKey: "1m",
	}

	obj := &unstructured.Unstructured{}
	obj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&deploy)

	kno := &KNativeServiceOwner{Interface: client}
	trueRef := kno.getTrueOwnerReference(deployref, obj)
	assert.NotNil(t, trueRef)
	objMap, err := kno.replace(*trueRef, obj, []string{deploymentReplicasPath})
	assert.ErrorIs(t, nil, err)
	assert.Equal(t, len(objMap), 1)
	for o, paths := range objMap {
		assert.Equal(t, o.GetName(), trueRef.Name)
		assert.Equal(t, len(paths), 1)
		assert.Equal(t, knativeSpecTemplateMetadataAnnotationsPath, paths[0])

		ksvc := &Service{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, ksvc)
		assert.ErrorIs(t, nil, err)
		assert.Equal(t, "0", ksvc.Spec.Template.Annotations[knativeMinScaleAnnotationKey])
		assert.Equal(t, "1", ksvc.Spec.Template.Annotations[knativeMaxScaleAnnotationKey])
	}

	err = testEnv.Stop()
	assert.ErrorIs(t, nil, err)
}

// This is not a standalone test case, but a function to be shared for obj setup for all cases
// Prepare Deployment -> Revision -> Configuration -> KService
// we don't convert deployment into unstructured due to the complexity of changing spec.replicas for each test case
func prepareKNativeResourcesForTest(t *testing.T, client dynamic.Interface) (*appsv1.Deployment, metav1.OwnerReference, error) {
	var err error

	ksvc := &Service{}
	ksvc.Name = _testKNativeServiceName_
	ksvc.Namespace = _testNamespace_
	ksvc.SetGroupVersionKind(knativeServiceGVK)
	ksvcObj := &unstructured.Unstructured{}
	ksvcObj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(ksvc)
	assert.ErrorIs(t, nil, err)
	ksvcObj, err = client.Resource(knativeServiceGVR).Namespace(ksvcObj.GetNamespace()).Create(context.TODO(), ksvcObj, metav1.CreateOptions{})
	assert.ErrorIs(t, nil, err)

	confObj := &unstructured.Unstructured{}
	confGVK := schema.GroupVersionKind{
		Group:   knativeRevisionGVR.Group,
		Version: knativeRevisionGVR.Version,
		Kind:    knativeConfigurationKind,
	}
	confObj.SetGroupVersionKind(confGVK)
	confObj.SetName(_testKNativeConfigurationName_)
	confObj.SetNamespace(_testNamespace_)
	confRef := metav1.OwnerReference{}
	confRef.APIVersion = knativeConfigurationsGVR.GroupVersion().String()
	confRef.Kind = knativeServiceGVK.Kind
	confRef.Name = _testKNativeServiceName_
	confRef.UID = ksvcObj.GetUID()
	confObj.SetOwnerReferences([]metav1.OwnerReference{confRef})
	confObj, err = client.Resource(knativeConfigurationsGVR).Namespace(_testNamespace_).Create(context.TODO(), confObj, metav1.CreateOptions{})
	assert.ErrorIs(t, nil, err)

	revObj := &unstructured.Unstructured{}
	revGVK := schema.GroupVersionKind{
		Group:   knativeRevisionGVR.Group,
		Version: knativeRevisionGVR.Version,
		Kind:    knativeRevisionKind,
	}
	revObj.SetGroupVersionKind(revGVK)
	revObj.SetName(_testKNativeRevisionName_)
	revObj.SetNamespace(_testNamespace_)
	revRef := metav1.OwnerReference{}
	revRef.APIVersion = knativeConfigurationsGVR.GroupVersion().String()
	revRef.Kind = knativeConfigurationKind
	revRef.Name = _testKNativeConfigurationName_
	revRef.UID = confObj.GetUID()
	revObj.SetOwnerReferences([]metav1.OwnerReference{revRef})
	revObj, err = client.Resource(knativeRevisionGVR).Namespace(_testNamespace_).Create(context.TODO(), revObj, metav1.CreateOptions{})
	assert.ErrorIs(t, nil, err)

	deploy := _testDeployment_.DeepCopy()
	deploy.Namespace = _testNamespace_
	var replicas int32 = 2
	deploy.Spec.Replicas = &replicas

	deployRef := metav1.OwnerReference{}
	deployRef.APIVersion = knativeRevisionGVR.GroupVersion().String()
	deployRef.Kind = knativeRevisionKind
	deployRef.Name = _testKNativeRevisionName_
	deploy.SetOwnerReferences([]metav1.OwnerReference{deployRef})

	return deploy, deployRef, nil
}
