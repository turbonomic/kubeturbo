package firstclass

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestCheckNamespaceQuotaForResources(t *testing.T) {
	testEnv := &envtest.Environment{}
	cfg, err := testEnv.Start()
	assert.NotNil(t, cfg)
	client := dynamic.NewForConfigOrDie(cfg)
	assert.NotNil(t, client)

	// no quota
	var replicas int64 = 4
	resource := corev1.ResourceList{
		"cpu": _testQuantityOne_,
	}
	err = checkNamespaceQuotaForResources(client, _testNamespace_, resource, replicas)
	assert.ErrorIs(t, nil, err)

	quotaObj := &unstructured.Unstructured{}
	quotaObj.Object, err = runtime.DefaultUnstructuredConverter.ToUnstructured(_testNamespaceQuota_.DeepCopy())
	assert.ErrorIs(t, nil, err)
	_, err = client.Resource(quotaGVR).Namespace(_testNamespace_).Create(context.TODO(), quotaObj, metav1.CreateOptions{})
	assert.ErrorIs(t, nil, err)
	_, err = client.Resource(quotaGVR).Namespace(_testNamespace_).UpdateStatus(context.TODO(), quotaObj, metav1.UpdateOptions{})
	assert.ErrorIs(t, nil, err)

	// enough quota
	err = checkNamespaceQuotaForResources(client, _testNamespace_, resource, replicas)
	assert.ErrorIs(t, nil, err)

	// not enough quota
	resource = corev1.ResourceList{
		"cpu": _testQuantityTwo_,
	}
	err = checkNamespaceQuotaForResources(client, _testNamespace_, resource, replicas)
	assert.ErrorContains(t, err, errorMessageNotEnoughQuota)

	err = testEnv.Stop()
	assert.ErrorIs(t, nil, err)
}

func TestKNativeCheckResourceForRolling(t *testing.T) {
	testEnv := &envtest.Environment{}
	cfg, err := testEnv.Start()
	assert.NotNil(t, cfg)
	client := dynamic.NewForConfigOrDie(cfg)
	assert.NotNil(t, client)

	prepareNodesForTesting(t, client)

	deploy := _testDeployment_.DeepCopy()

	affinity := &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{
		{
			MatchExpressions: []corev1.NodeSelectorRequirement{
				{
					Key:      _testNodeLableKeyHostName_,
					Operator: corev1.NodeSelectorOpIn,
					Values: []string{
						_testNodeName1_,
					},
				},
			},
		},
	}}

	deploy.Spec.Template.Spec.Affinity = &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: affinity,
		},
	}

	// enough resource on single node
	var replicas int32 = 2
	selectors := aggregatedNodeSelectorInDeployment(&deploy.Spec.Template)
	err = checkResourceForRolling(client, selectors, deploy.Spec.Template.Spec.Containers[0].Resources.Requests, int(replicas))
	assert.ErrorIs(t, nil, err)

	// not enough resource on single node
	replicas = 10
	err = checkResourceForRolling(client, selectors, deploy.Spec.Template.Spec.Containers[0].Resources.Requests, int(replicas))
	assert.ErrorContains(t, err, errorMessageMissingResource)

	// not enough resource on single node, but enough on multiple nodes
	affinity = &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{
		{
			MatchExpressions: []corev1.NodeSelectorRequirement{
				{
					Key:      _testNodeLableKeyHostName_,
					Operator: corev1.NodeSelectorOpIn,
					Values: []string{
						_testNodeName1_,
						_testNodeName2_,
					},
				},
			},
		},
	}}
	deploy.Spec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = affinity
	selectors = aggregatedNodeSelectorInDeployment(&deploy.Spec.Template)
	err = checkResourceForRolling(client, selectors, deploy.Spec.Template.Spec.Containers[0].Resources.Requests, int(replicas))
	assert.ErrorIs(t, nil, err)

	// not enough resource on all nodes
	replicas = 20
	deploy.Spec.Replicas = &replicas
	err = checkResourceForRolling(client, selectors, deploy.Spec.Template.Spec.Containers[0].Resources.Requests, int(replicas))
	assert.ErrorContains(t, err, errorMessageMissingResource)

	err = testEnv.Stop()
	assert.ErrorIs(t, nil, err)
}
