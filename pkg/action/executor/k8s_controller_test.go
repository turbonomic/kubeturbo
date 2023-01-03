package executor

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/action/executor/gitops"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	gitopsv1alpha1 "github.com/turbonomic/turbo-gitops/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var (
	mockNamespace          = "turbo"
	mockSecretName         = "test-secret"
	mockUsername           = "test"
	mockEmail              = "test@turbonomic"
	mockEmailOverride      = "override@turbonomic"
	mockCommitMode         = "request"
	mockCommitModeOverride = gitopsv1alpha1.DirectCommit
	mockWhitelist          = []string{"test1", "test2", "test3"}
	mockGitConfig          = gitops.GitConfig{
		GitSecretNamespace: mockNamespace,
		GitSecretName:      mockSecretName,
		GitUsername:        mockUsername,
		GitEmail:           mockEmail,
		CommitMode:         mockCommitMode,
	}
	mockGitOpsConfigurationSelector = gitopsv1alpha1.Configuration{
		CommitMode: mockCommitModeOverride,
		Selector:   "^.*TEST.*$",
	}
	mockGitOpsConfigurationWhitelist = gitopsv1alpha1.Configuration{
		CommitMode: mockCommitModeOverride,
		Whitelist:  mockWhitelist,
	}
	mockGitOpsConfigurationEmpty = gitopsv1alpha1.Configuration{
		CommitMode: mockCommitModeOverride,
	}
	mockGitOpsConfigurationCredentials = gitopsv1alpha1.Credentials{
		Email:           mockEmailOverride,
		Username:        mockUsername,
		SecretName:      mockSecretName,
		SecretNamespace: mockNamespace,
	}
	mockGitOpsConfigurationSelectorCredentials = gitopsv1alpha1.Configuration{
		CommitMode:  mockCommitModeOverride,
		Selector:    "^.*credentials.*$",
		Credentials: mockGitOpsConfigurationCredentials,
	}
	mockGitOpsConfigCache = map[string][]*gitopsv1alpha1.Configuration{
		mockNamespace: {
			&mockGitOpsConfigurationSelector,
			&mockGitOpsConfigurationWhitelist,
			&mockGitOpsConfigurationSelectorCredentials,
		},
	}
	mockObj = &unstructured.Unstructured{
		Object: map[string]interface{}{
			"metadata": map[string]interface{}{
				"namespace": mockNamespace}},
	}
	mockMutextLock       sync.Mutex
	mockParentController = &parentController{
		gitConfig:             mockGitConfig,
		gitOpsConfigCache:     &mockGitOpsConfigCache,
		gitOpsConfigCacheLock: &mockMutextLock,
	}
)

func TestIsAppSelectorMatch(t *testing.T) {
	assert.True(t, isAppSelectorMatch(".*", "TEST"))
	assert.True(t, isAppSelectorMatch("^.*$", "TEST"))
	assert.True(t, isAppSelectorMatch("^.*[TEST|test].*$", "TEST"))
	assert.False(t, isAppSelectorMatch("*", "TEST"))
	assert.False(t, isAppSelectorMatch("^app.*$", "TEST"))
	assert.False(t, isAppSelectorMatch("^.*app$", "TEST"))
	assert.False(t, isAppSelectorMatch("^.*test.*$", "TEST"))
}

func TestIsAppInWhitelist(t *testing.T) {
	whitelist := []string{"test1", "test2", "test3"}
	assert.True(t, isAppInWhitelist(whitelist, "test1"))
	assert.False(t, isAppInWhitelist(whitelist, "test4"))
}

func TestIsGitOpsConfigOverridden(t *testing.T) {
	assert.True(t, isGitOpsConfigOverridden(&mockGitOpsConfigurationSelector, "TEST"))
	assert.False(t, isGitOpsConfigOverridden(&mockGitOpsConfigurationSelector, "NOPE"))
	assert.True(t, isGitOpsConfigOverridden(&mockGitOpsConfigurationWhitelist, "test1"))
	assert.False(t, isGitOpsConfigOverridden(&mockGitOpsConfigurationWhitelist, "test4"))
	assert.False(t, isGitOpsConfigOverridden(&mockGitOpsConfigurationEmpty, "TEST"))
}

func getMockManagerApp(name string) *repository.K8sApp {
	return &repository.K8sApp{
		Name:      name,
		Namespace: mockNamespace,
	}
}

func TestGetGitOpsConfigOverriddenSelector(t *testing.T) {
	mockParentController.managerApp = getMockManagerApp("TEST")
	expectedConfig := gitops.GitConfig{
		GitSecretNamespace: mockNamespace,
		GitSecretName:      mockSecretName,
		GitUsername:        mockUsername,
		GitEmail:           mockEmail,
		CommitMode:         string(mockCommitModeOverride),
	}
	actualConfig := mockParentController.GetGitOpsConfig(mockObj)
	assert.Equal(t, expectedConfig, actualConfig)
}

func TestGetGitOpsConfigOverriddenWhitelist(t *testing.T) {
	mockParentController.managerApp = getMockManagerApp("test1")
	expectedConfig := gitops.GitConfig{
		GitSecretNamespace: mockNamespace,
		GitSecretName:      mockSecretName,
		GitUsername:        mockUsername,
		GitEmail:           mockEmail,
		CommitMode:         string(mockCommitModeOverride),
	}
	actualConfig := mockParentController.GetGitOpsConfig(mockObj)
	assert.Equal(t, expectedConfig, actualConfig)
}

func TestGetGitOpsConfigOverriddenCredentials(t *testing.T) {
	mockParentController.managerApp = getMockManagerApp("credentials")
	expectedConfig := gitops.GitConfig{
		GitSecretNamespace: mockNamespace,
		GitSecretName:      mockSecretName,
		GitUsername:        mockUsername,
		GitEmail:           mockEmailOverride,
		CommitMode:         string(mockCommitModeOverride),
	}
	actualConfig := mockParentController.GetGitOpsConfig(mockObj)
	assert.Equal(t, expectedConfig, actualConfig)
}

func TestGetGitOpsConfigDefault(t *testing.T) {
	mockParentController.managerApp = getMockManagerApp("default")
	actualConfig := mockParentController.GetGitOpsConfig(mockObj)
	assert.Equal(t, mockParentController.gitConfig, actualConfig)
}
