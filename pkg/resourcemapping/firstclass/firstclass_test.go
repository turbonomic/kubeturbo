package firstclass

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var (
	_testFirstClassOwnedObjectName_ = "ownedobj"
)

// test deployment owned by knative service which is owned by kserve inferenceservice
// depends on variable and functions defined in kserve_test and knative_test for env setup
func TestCheckAndReplaceWithFirstClassOwners(t *testing.T) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(".", "test_crds", "kserve"),
			filepath.Join(".", "test_crds", "knative"),
		},
		ErrorIfCRDPathMissing: true,
	}
	cfg, err := testEnv.Start()
	assert.NotNil(t, cfg)
	fcc, err := NewFirstClassClient(cfg)
	assert.ErrorIs(t, nil, err)

	prepareNodesForTesting(t, fcc.client)

	deploy, _, err := prepareKNativeResourcesForTest(t, fcc.client)
	assert.ErrorIs(t, nil, err)
	assert.NotNil(t, deploy)

	is := &InferenceService{}
	is.Namespace = _testNamespace_
	is.Name = _testInferenceServiceName_
	is.SetGroupVersionKind(kserveInferenceServiceGVK)
	isObj := &unstructured.Unstructured{}
	isObj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(is)
	assert.ErrorIs(t, nil, err)
	isObj, err = fcc.client.Resource(kserveInferenceServiceGVR).Namespace(isObj.GetNamespace()).Create(context.TODO(), isObj, metav1.CreateOptions{})
	assert.ErrorIs(t, nil, err)

	ksvcRef := metav1.OwnerReference{}
	ksvcRef.APIVersion, ksvcRef.Kind = kserveInferenceServiceGVK.ToAPIVersionAndKind()
	ksvcRef.Name = _testInferenceServiceName_
	ksvcRef.UID = isObj.GetUID()

	ksvcObj, err := fcc.client.Resource(knativeServiceGVR).Namespace(_testNamespace_).Get(context.TODO(), _testKNativeServiceName_, metav1.GetOptions{})
	assert.ErrorIs(t, nil, err)
	ksvcObj.SetOwnerReferences([]metav1.OwnerReference{ksvcRef})
	ksvcObj.SetLabels(
		map[string]string{
			inferenceServiceComponentLabelName: _testInferenceServiceComponentName_,
		},
	)
	_, err = fcc.client.Resource(knativeServiceGVR).Namespace(_testNamespace_).Update(context.TODO(), ksvcObj, metav1.UpdateOptions{})
	assert.ErrorIs(t, nil, err)

	var replicas int32 = 3
	deploy.Spec.Replicas = &replicas

	obj := &unstructured.Unstructured{}
	obj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&deploy)
	assert.ErrorIs(t, nil, err)

	objMap, err := fcc.CheckAndReplaceWithFirstClassOwners(obj, []string{deploymentReplicasPath})
	assert.ErrorIs(t, nil, err)
	assert.Greater(t, len(objMap), 0)
	for o, paths := range objMap {
		assert.Equal(t, _testInferenceServiceName_, o.GetName())
		assert.Equal(t, 2, len(paths))
		assert.NotEqual(t, paths[0], paths[1])

		is := &InferenceService{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(o.Object, is)
		assert.ErrorIs(t, nil, err)
		assert.Equal(t, 3, is.Spec.Predictor.MaxReplicas)
		assert.Equal(t, 3, *is.Spec.Predictor.MinReplicas)
	}

	err = testEnv.Stop()
	assert.ErrorIs(t, nil, err)
}

func TestIdentifyFirstClassOwner(t *testing.T) {
	var err error

	fcc := FirstClasssClient{
		registry: firstClassRegistry{
			kserveInferenceServiceGVK: newKServeInferenceServiceOwner(nil),
			knativeServiceGVK:         newKNativeOwner(nil),
		},
	}

	deploy := appsv1.Deployment{}
	deploy.Name = _testFirstClassOwnedObjectName_
	var replicas int32 = 3
	deploy.Spec.Replicas = &replicas

	obj := &unstructured.Unstructured{}
	obj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(&deploy)
	assert.ErrorIs(t, nil, err)

	// case 1: not first class owner
	ref := metav1.OwnerReference{
		APIVersion: "somegroup/v1",
		Kind:       "somekind",
		Name:       "somename",
	}

	obj.SetOwnerReferences([]metav1.OwnerReference{ref})
	fco, err := fcc.identifyFirstClassOwners(obj)
	assert.ErrorIs(t, nil, err)
	assert.Equal(t, 0, len(fco))

	// case 2: kserve owner
	ref.APIVersion, ref.Kind = kserveInferenceServiceGVK.ToAPIVersionAndKind()
	obj.SetOwnerReferences([]metav1.OwnerReference{ref})
	fco, err = fcc.identifyFirstClassOwners(obj)
	assert.ErrorIs(t, nil, err)
	assert.Equal(t, 1, len(fco))
	for k, v := range fco {
		assert.Equal(t, ref, k)
		assert.Equal(t, kserveInferenceServiceGVK, v)
	}

	// case 3: knative owner
	ref.APIVersion, ref.Kind = knativeServiceGVK.ToAPIVersionAndKind()
	obj.SetOwnerReferences([]metav1.OwnerReference{ref})
	fco, err = fcc.identifyFirstClassOwners(obj)
	assert.ErrorIs(t, nil, err)
	assert.Equal(t, 1, len(fco))
	for k, v := range fco {
		assert.Equal(t, ref, k)
		assert.Equal(t, knativeServiceGVK, v)
	}

}

func prepareNodesForTesting(t *testing.T, client dynamic.Interface) {

	var err error

	nodeObj := &unstructured.Unstructured{}
	nodeObj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(_testNode1_.DeepCopy())
	assert.ErrorIs(t, nil, err)
	_, err = client.Resource(nodeGVR).Create(context.TODO(), nodeObj, metav1.CreateOptions{})
	assert.ErrorIs(t, nil, err)
	nodeObj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(_testNode2_.DeepCopy())
	assert.ErrorIs(t, nil, err)
	_, err = client.Resource(nodeGVR).Create(context.TODO(), nodeObj, metav1.CreateOptions{})
	assert.ErrorIs(t, nil, err)

}
