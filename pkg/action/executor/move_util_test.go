package executor

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	commonutil "github.ibm.com/turbonomic/kubeturbo/pkg/util"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_getOwner(t *testing.T) {
	tests := []struct {
		name            string
		ownerReferences []metav1.OwnerReference
		expectedResult  bool
	}{
		{
			name:            "testReplicaSetOwner",
			ownerReferences: []metav1.OwnerReference{{Kind: commonutil.KindReplicaSet}},
			expectedResult:  true,
		},
		{
			name:            "testNodeOwner",
			ownerReferences: []metav1.OwnerReference{{Kind: commonutil.KindNode}},
			expectedResult:  false,
		},
		{
			name:            "testNilOwner",
			ownerReferences: nil,
			expectedResult:  false,
		},
		{
			name:            "testEmptyOwnerReferences",
			ownerReferences: nil,
			expectedResult:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pod := v1.Pod{}
			pod.OwnerReferences = test.ownerReferences

			_, actualResult := getPodOwner(&pod)

			assert.Equal(t, test.expectedResult, actualResult)
		})
	}
}

func Test_generatePodHash(t *testing.T) {
	lastHash := "TEST"
	for i := 0; i < 100; i++ {
		newHash := generatePodHash()
		assert.NotEqual(t, lastHash, newHash)
		lastHash = newHash
	}
}

func Test_genNewPodName(t *testing.T) {
	tests := []struct {
		name            string
		podName         string
		ownerReferences []metav1.OwnerReference
		assertLength    bool
	}{
		{
			name:            "testLongDeploymentName",
			podName:         "this-is-a-really-really-long-deployment-name-644c979b95-sqr68",
			ownerReferences: []metav1.OwnerReference{{Kind: commonutil.KindReplicaSet, Name: "this-is-a-really-really-long-deployment-name-644c979b95"}},
			assertLength:    true,
		},
		{
			name:            "testBarePodName",
			podName:         "bare-pod",
			ownerReferences: nil,
			assertLength:    false,
		},
		{
			name:            "testPreviouslyMovedPod",
			podName:         "test-pod-64fb879689-9prv4-12t7ftmlhgo00",
			ownerReferences: []metav1.OwnerReference{{Kind: commonutil.KindReplicaSet, Name: "test-pod-64fb879689"}},
			assertLength:    true,
		},
		{
			name:            "testPreviouslyMovedPod2",
			podName:         "test-pod-64fb879689-9prv4-12t7f",
			ownerReferences: []metav1.OwnerReference{{Kind: commonutil.KindReplicaSet, Name: "test-pod-64fb879689"}},
			assertLength:    true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pod := v1.Pod{}
			pod.Name = test.podName
			pod.OwnerReferences = test.ownerReferences

			newName := genNewPodName(&pod)

			assert.NotEqual(t, pod.Name, newName)
			if test.assertLength {
				assert.True(t, len(newName) <= len(pod.Name))
			}

			var newPodHash string
			if idx := strings.LastIndex(newName, "-"); idx >= 0 {
				newPodHash = newName[idx+1:]
			}

			assert.Equal(t, 5, len(newPodHash))

			if test.ownerReferences != nil {
				var oldPodHash string
				if idx := strings.LastIndex(pod.Name, "-"); idx >= 0 {
					oldPodHash = pod.Name[idx+1:]
				}
				assert.NotNil(t, oldPodHash)
				assert.NotNil(t, newPodHash)
				assert.NotEqual(t, oldPodHash, newPodHash)
				assert.Equal(t, len(newName), len(pod.OwnerReferences[0].Name)+6) // hyphen + hash
			}
		})
	}
}
