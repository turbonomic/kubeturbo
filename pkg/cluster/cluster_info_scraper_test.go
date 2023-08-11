package cluster

import (
	machinev1beta1api "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	api "k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"testing"

	"github.com/stretchr/testify/assert"
	gitopsv1alpha1 "github.com/turbonomic/turbo-gitops/api/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	mockTypeMeta = v1.TypeMeta{
		Kind:       "GitOps",
		APIVersion: "gitops.turbonomic.io/v1alpha1",
	}
	mockObjectMeta = v1.ObjectMeta{
		Name:      "gitops-config-test",
		Namespace: "turbonomic",
		UID:       "c05990b9-e0f8-43b8-8f09-f4223c6711c9",
	}
)

// Implements the ClusterScrapperInterface.
type MockClusterScrapper struct {
	mockGetAllGitOpsConfigurations func() ([]gitopsv1alpha1.GitOps, error)
}

func TestGetAllGitOpsConfigurationsCommitMode(t *testing.T) {
	mockClusterScraper := &MockClusterScrapper{
		mockGetAllGitOpsConfigurations: func() ([]gitopsv1alpha1.GitOps, error) {
			return []gitopsv1alpha1.GitOps{
				{
					TypeMeta:   mockTypeMeta,
					ObjectMeta: mockObjectMeta,
					Spec: gitopsv1alpha1.GitOpsSpec{
						Configuration: []gitopsv1alpha1.Configuration{
							{CommitMode: "request"},
						},
					},
				},
			}, nil
		},
	}
	gitOpsConfigs, err := mockClusterScraper.mockGetAllGitOpsConfigurations()
	assert.Nil(t, err)
	assert.NotEmpty(t, gitOpsConfigs)
	assert.NotEmpty(t, gitOpsConfigs[0].Spec.Configuration)
	assert.Equal(t, gitopsv1alpha1.CommitMode("request"), gitOpsConfigs[0].Spec.Configuration[0].CommitMode)
}

func TestGetAllGitOpsConfigurationsCredentials(t *testing.T) {
	mockEmail := "mockEmail"
	mockSecretName := "mockSecretName"
	mockSecretNamespace := "mockSecretNamespace"
	mockUsername := "mockUsername"

	mockClusterScraper := &MockClusterScrapper{
		mockGetAllGitOpsConfigurations: func() ([]gitopsv1alpha1.GitOps, error) {
			return []gitopsv1alpha1.GitOps{
				{
					TypeMeta:   mockTypeMeta,
					ObjectMeta: mockObjectMeta,
					Spec: gitopsv1alpha1.GitOpsSpec{
						Configuration: []gitopsv1alpha1.Configuration{
							{Credentials: gitopsv1alpha1.Credentials{
								Email:           mockEmail,
								SecretName:      mockSecretName,
								SecretNamespace: mockSecretNamespace,
								Username:        mockUsername,
							}},
						},
					},
				},
			}, nil
		},
	}
	gitOpsConfigs, err := mockClusterScraper.mockGetAllGitOpsConfigurations()
	assert.Nil(t, err)
	assert.NotEmpty(t, gitOpsConfigs)
	assert.NotEmpty(t, gitOpsConfigs[0].Spec.Configuration)
	assert.Equal(t, mockEmail, gitOpsConfigs[0].Spec.Configuration[0].Credentials.Email)
	assert.Equal(t, mockSecretName, gitOpsConfigs[0].Spec.Configuration[0].Credentials.SecretName)
	assert.Equal(t, mockSecretNamespace, gitOpsConfigs[0].Spec.Configuration[0].Credentials.SecretNamespace)
	assert.Equal(t, mockUsername, gitOpsConfigs[0].Spec.Configuration[0].Credentials.Username)
}

func TestGetAllGitOpsConfigurationsSelector(t *testing.T) {
	mockSelector := "*"
	mockClusterScraper := &MockClusterScrapper{
		mockGetAllGitOpsConfigurations: func() ([]gitopsv1alpha1.GitOps, error) {
			return []gitopsv1alpha1.GitOps{
				{
					TypeMeta:   mockTypeMeta,
					ObjectMeta: mockObjectMeta,
					Spec: gitopsv1alpha1.GitOpsSpec{
						Configuration: []gitopsv1alpha1.Configuration{
							{Selector: mockSelector},
						},
					},
				},
			}, nil
		},
	}
	gitOpsConfigs, err := mockClusterScraper.mockGetAllGitOpsConfigurations()
	assert.Nil(t, err)
	assert.NotEmpty(t, gitOpsConfigs)
	assert.NotEmpty(t, gitOpsConfigs[0].Spec.Configuration)
	assert.Equal(t, mockSelector, gitOpsConfigs[0].Spec.Configuration[0].Selector)
}

func TestGetAllGitOpsConfigurationsWhitelist(t *testing.T) {
	mockApp1 := "mockApp1"
	mockApp2 := "mockApp2"

	mockClusterScraper := &MockClusterScrapper{
		mockGetAllGitOpsConfigurations: func() ([]gitopsv1alpha1.GitOps, error) {
			return []gitopsv1alpha1.GitOps{
				{
					TypeMeta:   mockTypeMeta,
					ObjectMeta: mockObjectMeta,
					Spec: gitopsv1alpha1.GitOpsSpec{
						Configuration: []gitopsv1alpha1.Configuration{
							{Whitelist: []string{mockApp1, mockApp2}},
						},
					},
				},
			}, nil
		},
	}
	gitOpsConfigs, err := mockClusterScraper.mockGetAllGitOpsConfigurations()
	assert.Nil(t, err)
	assert.NotEmpty(t, gitOpsConfigs)
	assert.NotEmpty(t, gitOpsConfigs[0].Spec.Configuration)
	assert.Equal(t, 2, len(gitOpsConfigs[0].Spec.Configuration[0].Whitelist))
	assert.Contains(t, gitOpsConfigs[0].Spec.Configuration[0].Whitelist, mockApp1)
	assert.Contains(t, gitOpsConfigs[0].Spec.Configuration[0].Whitelist, mockApp2)
}

func TestGetMachineSetToNodesMap(t *testing.T) {
	machineTypeMeta := metav1.TypeMeta{
		Kind:       "Machine",
		APIVersion: "machine.openshift.io/v1beta1",
	}

	machines := []machinev1beta1api.Machine{
		{
			TypeMeta: machineTypeMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker-1",
				Namespace: "test",
				UID:       "1",
			},

			Status: machinev1beta1api.MachineStatus{
				NodeRef: &corev1.ObjectReference{
					Kind:      "Node",
					Namespace: "",
					Name:      "worker-1",
					UID:       "1",
				},
			},
		},
		{
			TypeMeta: machineTypeMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:      "worker-2",
				Namespace: "test",
				UID:       "2",
			},
			Status: machinev1beta1api.MachineStatus{
				NodeRef: &corev1.ObjectReference{
					Kind:      "Node",
					Namespace: "",
					Name:      "worker-2",
					UID:       "2",
				},
			},
		},
	}

	labels := map[string]string{
		"machine.openshift.io/cluster-api-machineset": "ocp-release-jlw72-worker",
		"machine.openshift.io/cluster-api-cluster":    "ocp-release-jlw72",
	}
	allNodes := []*api.Node{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "master-0",
				UID:    "1",
				Labels: labels,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "master-1",
				UID:  "2",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "master-2",
				UID:  "3",
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "worker-1",
				UID:    "4",
				Labels: labels,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "worker-2",
				UID:    "5",
				Labels: labels,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "worker-3",
				UID:    "6",
				Labels: labels,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "worker-4",
				UID:    "7",
				Labels: labels,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "worker-5",
				UID:    "8",
				Labels: labels,
			},
		},
	}
	nodes := getNodesFromMachines(machines, allNodes)
	assert.NotEmpty(t, nodes)
	assert.Equal(t, len(nodes), 2)
}

func TestGetNewConfigValue(t *testing.T) {
	getKeyStringVal := func(key string) string {
		switch key {
		case "empty":
			return ""
		case "negative":
			return "-10"
		case "invalid":
			return "abc"
		case "good":
			return "42"
		default:
			return ""
		}
	}

	defaultValue := 10

	tests := []struct {
		configKey      string
		expectedResult int
	}{
		{"empty", defaultValue},       // Empty string should return default value
		{"negative", defaultValue},    // Negative number should return default value
		{"invalid", defaultValue},     // Invalid input should return default value
		{"good", 42},                  // Good number should return the parsed value
		{"nonexistent", defaultValue}, // Nonexistent key should return default value
	}

	for _, test := range tests {
		result := GetNodePoolSizeConfigValue(test.configKey, getKeyStringVal, defaultValue)
		if result != test.expectedResult {
			t.Errorf("For key '%s', expected: %d, but got: %d", test.configKey, test.expectedResult, result)
		}
	}
}
