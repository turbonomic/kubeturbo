package firstclass

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/stretchr/testify/assert"
	"github.ibm.com/turbonomic/orm/utils"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
)

// define inference service struct locally for testing only and to avoid huge amount of packages
// need to check and follow official implementation
type InferenceService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InferenceServiceSpec   `json:"spec,omitempty"`
	Status InferenceServiceStatus `json:"status,omitempty"`
}

type InferenceServiceComponentExtensionSpec struct {
	MinReplicas *int `json:"minReplicas,omitempty"`
	MaxReplicas int  `json:"maxReplicas,omitempty"`
}

type InferenceServiceSpec struct {
	Predictor   InferenceServiceComponentSpecSpec  `json:"predictor"`
	Explainer   *InferenceServiceComponentSpecSpec `json:"explainer,omitempty"`
	Transformer *InferenceServiceComponentSpecSpec `json:"transformer,omitempty"`
}

type InferenceServiceComponentSpecSpec struct {
	InferenceServiceComponentExtensionSpec `json:",inline"`
}

type InferenceServiceStatus struct {
}

var (
	_testInferenceServiceName_ = "test-inference"

	_testInferenceServiceComponentName_  = "predictor"
	_testInferenceServiceDeploymentName_ = _testInferenceServiceName_ + "-" + _testInferenceServiceComponentName_
)

// replace object and paths with InferenceService
func TestInferenceServiceReplaceWithDeployment(t *testing.T) {

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(".", "test_crds", "kserve"),
		},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	assert.NotNil(t, cfg)
	client := dynamic.NewForConfigOrDie(cfg)
	assert.NotNil(t, client)

	is := &InferenceService{}
	is.Namespace = _testNamespace_
	is.Name = _testInferenceServiceName_
	is.SetGroupVersionKind(kserveInferenceServiceGVK)
	isObj := &unstructured.Unstructured{}
	isObj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(is)
	assert.ErrorIs(t, nil, err)
	_, err = client.Resource(kserveInferenceServiceGVR).Namespace(isObj.GetNamespace()).Create(context.TODO(), isObj, metav1.CreateOptions{})
	assert.ErrorIs(t, nil, err)

	deploy := appsv1.Deployment{}
	deploy.SetGroupVersionKind(deploymentGVK)
	deploy.Name = _testInferenceServiceDeploymentName_
	deploy.Namespace = _testNamespace_
	deploy.Labels = map[string]string{
		inferenceServiceComponentLabelName: _testInferenceServiceComponentName_,
	}
	var replicas int32 = 3
	deploy.Spec.Replicas = &replicas

	obj := &unstructured.Unstructured{}
	obj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&deploy)
	assert.ErrorIs(t, nil, err)

	ref := metav1.OwnerReference{}
	ref.APIVersion, ref.Kind = kserveInferenceServiceGVK.ToAPIVersionAndKind()
	ref.Name = _testInferenceServiceName_
	obj.SetOwnerReferences([]metav1.OwnerReference{ref})

	kso := &KServeInferenceServiceOwner{Interface: client}
	objMap, err := kso.replace(ref, obj, []string{deploymentReplicasPath})
	assert.ErrorIs(t, nil, err)
	assert.Greater(t, len(objMap), 0)
	for o, paths := range objMap {
		assert.Equal(t, _testInferenceServiceName_, o.GetName())
		assert.Equal(t, len(paths), 2)
		assert.NotEqual(t, paths[0], paths[1])

		rep, exists, err := utils.NestedField(o.Object, paths[0])
		assert.ErrorIs(t, nil, err)
		assert.True(t, exists)
		assert.Equal(t, int(rep.(int64)), 3, "path:", paths[0])

		rep, exists, err = utils.NestedField(o.Object, paths[1])
		assert.ErrorIs(t, nil, err)
		assert.True(t, exists)
		assert.Equal(t, int(rep.(int64)), 3, "path:", paths[1])

		is := &InferenceService{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, is)
		assert.ErrorIs(t, nil, err)
		assert.Equal(t, is.Spec.Predictor.MaxReplicas, 3)
	}

	err = testEnv.Stop()
	assert.ErrorIs(t, nil, err)
}

// This test depends on knative_test functions
func TestInferenceServiceReplaceWithKNativeService(t *testing.T) {

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(".", "test_crds", "kserve"),
			filepath.Join(".", "test_crds", "knative"),
		},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	assert.NotNil(t, cfg)
	client := dynamic.NewForConfigOrDie(cfg)
	assert.NotNil(t, client)

	is := &InferenceService{}
	is.Namespace = _testNamespace_
	is.Name = _testInferenceServiceName_
	is.SetGroupVersionKind(kserveInferenceServiceGVK)
	isObj := &unstructured.Unstructured{}
	isObj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(is)
	assert.ErrorIs(t, nil, err)
	isObj, err = client.Resource(kserveInferenceServiceGVR).Namespace(isObj.GetNamespace()).Create(context.TODO(), isObj, metav1.CreateOptions{})
	assert.ErrorIs(t, nil, err)

	ksvc := &Service{}
	ksvc.SetGroupVersionKind(knativeServiceGVK)
	ksvc.Name = _testKNativeServiceName_
	ksvc.Namespace = _testNamespace_
	ksvc.Labels = map[string]string{
		inferenceServiceComponentLabelName: _testInferenceServiceComponentName_,
	}
	min := 1
	max := 3
	ksvcTemplateAnnotation := map[string]string{
		knativeMinScaleAnnotationKey: fmt.Sprintf("%d", min),
		knativeMaxScaleAnnotationKey: fmt.Sprintf("%d", max),
	}
	ksvc.Spec.Template.Annotations = ksvcTemplateAnnotation
	ksvcObj := &unstructured.Unstructured{}
	ksvcObj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(ksvc)
	assert.ErrorIs(t, nil, err)
	ksvcRef := metav1.OwnerReference{}
	ksvcRef.APIVersion = knativeConfigurationsGVR.GroupVersion().String()
	ksvcRef.Kind = knativeServiceGVK.Kind
	ksvcRef.Name = _testInferenceServiceName_
	ksvcRef.UID = isObj.GetUID()
	ksvcObj.SetOwnerReferences([]metav1.OwnerReference{ksvcRef})

	kso := &KServeInferenceServiceOwner{Interface: client}
	objMap, err := kso.replace(ksvcRef, ksvcObj, []string{knativeSpecTemplateMetadataAnnotationsPath})
	assert.ErrorIs(t, nil, err)
	assert.Greater(t, len(objMap), 0)
	for o, paths := range objMap {
		assert.Equal(t, _testInferenceServiceName_, o.GetName())
		assert.Equal(t, len(paths), 2)
		assert.NotEqual(t, paths[0], paths[1])

		is := &InferenceService{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, is)
		assert.ErrorIs(t, nil, err)
		assert.Equal(t, max, is.Spec.Predictor.MaxReplicas)
		assert.Equal(t, min, *is.Spec.Predictor.MinReplicas)
	}

	err = testEnv.Stop()
	assert.ErrorIs(t, nil, err)
}
