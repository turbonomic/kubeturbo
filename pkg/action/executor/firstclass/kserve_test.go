package firstclass

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.ibm.com/turbonomic/orm/utils"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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
	_testInferenceServiceName_           = "test-inference"
	_testInferenceServiceComponentName_  = "predictor"
	_testInferenceServiceDeploymentName_ = _testInferenceServiceName_ + "-" + _testInferenceServiceComponentName_
)

// pass back original obj and paths if not owned by first class citizens
func TestFirstClassNotOwner(t *testing.T) {
	var err error
	deploy := appsv1.Deployment{}
	deploy.Name = _testInferenceServiceDeploymentName_
	var replicas int32 = 3
	deploy.Spec.Replicas = &replicas

	obj := &unstructured.Unstructured{}
	var bytes []byte
	bytes, err = json.Marshal(deploy)
	assert.ErrorIs(t, err, nil)
	obj.UnmarshalJSON(bytes)
	assert.ErrorIs(t, err, nil)

	paths := []string{inferenceServiceDeploymentReplicasPath}
	objMap := CheckAndReplaceWithFirstClassOwners(obj, paths)
	assert.Equal(t, len(objMap), 1)
	for o, p := range objMap {
		assert.Equal(t, obj, o)
		assert.Equal(t, paths, p)
	}
}

// replace object and paths with InferenceService
func TestInferenceService(t *testing.T) {
	var err error
	deploy := appsv1.Deployment{}
	deploy.Name = _testInferenceServiceDeploymentName_
	deploy.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: inferenceServiceAPIVersion,
			Kind:       inferenceServiceKind,
			Name:       _testInferenceServiceName_,
		},
	}
	deploy.Labels = map[string]string{
		inferenceServiceComponentLabelName: _testInferenceServiceComponentName_,
	}
	var replicas int32 = 3
	deploy.Spec.Replicas = &replicas

	obj := &unstructured.Unstructured{}
	var bytes []byte
	bytes, err = json.Marshal(deploy)
	assert.ErrorIs(t, err, nil)
	obj.UnmarshalJSON(bytes)
	assert.ErrorIs(t, err, nil)

	objmap := checkAndReplaceWithInferenceService(obj, []string{inferenceServiceDeploymentReplicasPath})
	assert.Greater(t, len(objmap), 0)
	for o, paths := range objmap {
		assert.Equal(t, o.GetName(), _testInferenceServiceName_)
		assert.Equal(t, len(paths), 2)
		assert.NotEqual(t, paths[0], paths[1])

		rep, exists, err := utils.NestedField(o.Object, paths[0])
		assert.ErrorIs(t, err, nil)
		assert.True(t, exists)
		assert.Equal(t, int(rep.(int64)), 3, "path:", paths[0])

		rep, exists, err = utils.NestedField(o.Object, paths[1])
		assert.ErrorIs(t, err, nil)
		assert.True(t, exists)
		assert.Equal(t, int(rep.(int64)), 3, "path:", paths[1])

		is := &InferenceService{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, is)
		assert.ErrorIs(t, err, nil)
		assert.Equal(t, is.Spec.Predictor.MaxReplicas, 3)
	}
}
