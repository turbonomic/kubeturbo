package cluster

import (
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
