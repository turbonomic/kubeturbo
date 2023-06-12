package executor

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"

	osv1 "github.com/openshift/api/apps/v1"
	osclient "github.com/openshift/client-go/apps/clientset/versioned"
	apicorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/dynamic"
	typedClient "k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/action/executor/gitops"
	actionutil "github.com/turbonomic/kubeturbo/pkg/action/util"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	discoveryutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/features"
	"github.com/turbonomic/kubeturbo/pkg/util"
	gitopsv1alpha1 "github.com/turbonomic/turbo-gitops/api/v1alpha1"
)

// k8sController defines a common interface for kubernetes controller actions
// Currently supported controllers include:
// - ReplicationController
// - ReplicaSet
// - Deployment
type k8sController interface {
	get(name string) (*k8sControllerSpec, error)
	update(updatedSpec *k8sControllerSpec) error
	revert() error
}

// k8sControllerSpec defines a set of objects that we want to update:
// - replicas: The replicas of a controller to update for horizontal scale
// - podSpec: The pod template of a controller to update for consistent resize
// Note: Use pointer for in-place update
type k8sControllerSpec struct {
	replicas       *int32
	podSpec        *apicorev1.PodSpec
	controllerName string
}

type kubeClients struct {
	typedClient *typedClient.Clientset
	osClient    *osclient.Clientset
	dynClient   dynamic.Interface
	// TODO: remove the need of this as we have dynClient already
	dynNamespacedClient dynamic.ResourceInterface
}

type parentController struct {
	clients               kubeClients
	obj                   *unstructured.Unstructured
	backupObj             *unstructured.Unstructured
	name                  string
	ormClient             *resourcemapping.ORMClientManager
	managerApp            *repository.K8sApp
	gitConfig             gitops.GitConfig
	k8sClusterId          string
	KubeCluster           *repository.KubeCluster
	gitOpsConfigCache     map[string][]*gitopsv1alpha1.Configuration
	gitOpsConfigCacheLock *sync.Mutex
}

func (c *parentController) get(name string) (*k8sControllerSpec, error) {
	obj, err := c.clients.dynNamespacedClient.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	objName := fmt.Sprintf("%s/%s", obj.GetNamespace(), name)
	kind := obj.GetKind()

	replicas := int64(0)
	found := false
	if kind != util.KindDaemonSet { // daemonsets do not have replica field
		replicas, found, err = unstructured.NestedInt64(obj.Object, "spec", "replicas")
		if err != nil || !found {
			return nil, fmt.Errorf("error retrieving replicas from %s %s: %v", kind, objName, err)
		}
	}

	podSpecUnstructured, found, err := unstructured.NestedFieldCopy(obj.Object, "spec", "template", "spec")
	if err != nil || !found {
		return nil, fmt.Errorf("error retrieving podSpec from %s %s: %v", kind, objName, err)
	}

	podSpec := apicorev1.PodSpec{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(podSpecUnstructured.(map[string]interface{}), &podSpec); err != nil {
		return nil, fmt.Errorf("error converting unstructured pod spec to typed pod spec for %s %s: %v", kind, objName, err)
	}

	c.obj = obj
	c.backupObj = c.obj.DeepCopy()
	int32Replicas := int32(replicas)
	return &k8sControllerSpec{
		replicas:       &int32Replicas,
		podSpec:        &podSpec,
		controllerName: fmt.Sprintf("%s-%s", kind, objName),
	}, nil
}

func (c *parentController) update(updatedSpec *k8sControllerSpec) error {
	objName := fmt.Sprintf("%s/%s", c.obj.GetNamespace(), c.obj.GetName())
	kind := c.obj.GetKind()

	replicaVal := int64(*updatedSpec.replicas)
	podSpecUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(updatedSpec.podSpec)
	if err != nil {
		return fmt.Errorf("error converting pod spec to unstructured pod spec for %s %s: %v", kind, objName, err)
	}
	if kind != util.KindDaemonSet { // daemonsets do not have replica field
		if err := unstructured.SetNestedField(c.obj.Object, replicaVal, "spec", "replicas"); err != nil {
			return fmt.Errorf("error setting replicas into unstructured %s %s: %v", kind, objName, err)
		}
	}
	if err := unstructured.SetNestedField(c.obj.Object, podSpecUnstructured, "spec", "template", "spec"); err != nil {
		return fmt.Errorf("error setting podSpec into unstructured %s %s: %v", kind, objName, err)
	}

	if c.managerApp != nil &&
		c.managerApp.Type != repository.AppTypeK8s &&
		utilfeature.DefaultFeatureGate.Enabled(features.GitopsApps) {
		var manager gitops.GitopsManager
		switch c.managerApp.Type {
		case repository.AppTypeArgoCD:
			gitOpsConfig := c.GetGitOpsConfig(c.obj)
			// The workload is managed by a pipeline controller (argoCD) which replicates
			// it from a source of truth
			manager = gitops.NewGitManager(gitOpsConfig, c.clients.typedClient,
				c.clients.dynClient, c.obj, c.managerApp, c.k8sClusterId)
			glog.Infof("Gitops pipeline detected.")
		default:
			return fmt.Errorf("unsupported gitops manager type: %v", c.managerApp.Type)
		}

		completionFn, completionData, err := manager.Update(int64(*updatedSpec.replicas), podSpecUnstructured)
		if err != nil {
			return fmt.Errorf("failed to update the gitops managed source of truth: %v", err)
		}
		return manager.WaitForActionCompletion(completionFn, completionData)
	}

	ownerInfo, isOwnerSet := discoveryutil.GetOwnerInfo(c.obj.GetOwnerReferences())
	if !c.shouldSkipOperator(c.obj) && isOwnerSet {
		// If k8s controller is controlled by custom controller, update the CR using OperatorResourceMapping
		// if SkipOperatorLabel is not set or not true.
		if c.ormClient == nil {
			return fmt.Errorf("failed to execute action with nil ORMClient")
		}
		resourcePaths, err := getResourcePath(kind, updatedSpec)
		if err != nil {
			return fmt.Errorf("unable to get resource paths: %v", err)
		}
		ownerResources, err := c.ormClient.GetOwnerResourcesForSource(c.obj, ownerInfo, resourcePaths)
		if err != nil {
			return fmt.Errorf("unable to get owner resources: %v", err)
		}
		return c.ormClient.UpdateOwners(c.obj, ownerInfo, ownerResources)
	} else {
		start := time.Now()
		_, err = c.clients.dynNamespacedClient.Update(context.TODO(), c.obj, metav1.UpdateOptions{})
		if err != nil {
			return err
		}

		if utilfeature.DefaultFeatureGate.Enabled(features.ForceDeploymentConfigRollout) {
			needsRollout, typedDC, err := c.shouldRolloutDeploymentConfig()
			if err != nil {
				return err
			}
			if needsRollout {
				err = c.rolloutDeploymentConfig(typedDC)
				if err != nil {
					return err
				}
			}
		}

		if utilfeature.DefaultFeatureGate.Enabled(features.AllowIncreaseNsQuota4Resizing) {
			err4Waiting := c.waitForAllNewReplicasToBeCreated(c.obj.GetName())
			if err4Waiting != nil {
				glog.V(2).Infof("Get error while waiting for the new replicas to be created for the workload controller %s/%s:%v", c.obj.GetNamespace(), c.obj.GetName(), err4Waiting)
				if hasWarningEvent, warningInfo := c.getLatestWarningEventsSinceUpdate(c.obj.GetNamespace(), c.obj.GetName(), start); hasWarningEvent {
					return fmt.Errorf(warningInfo)
				}
				return err4Waiting
			}
			glog.V(2).Infof("All of the new replicasets get created for the workload controller %s/%s", c.obj.GetNamespace(), c.obj.GetName())
		}
	}
	return err
}

func (c *parentController) revert() error {
	oldPodSpecUnstructured, found, err := unstructured.NestedFieldCopy(c.backupObj.Object, "spec", "template", "spec")
	if err != nil || !found {
		return err
	}
	currentObj, err := c.clients.dynNamespacedClient.Get(context.TODO(), c.obj.GetName(), metav1.GetOptions{})
	if err != nil {
		return err
	}
	currentPodSpecUnstructured, found, err := unstructured.NestedFieldCopy(currentObj.Object, "spec", "template", "spec")
	if err != nil || !found {
		return err
	}
	if reflect.DeepEqual(oldPodSpecUnstructured, currentPodSpecUnstructured) {
		glog.V(4).Infof("There is no change between the original pod spec and the current one for the workload controller %s/%s, no need to revert", c.obj.GetNamespace(), c.obj.GetName())
		return nil
	}
	if err := unstructured.SetNestedField(currentObj.Object, oldPodSpecUnstructured, "spec", "template", "spec"); err != nil {
		return err
	}
	if _, err = c.clients.dynNamespacedClient.Update(context.TODO(), currentObj, metav1.UpdateOptions{}); err != nil {
		return err
	}
	return nil
}

func (c *parentController) rolloutDeploymentConfig(dc *osv1.DeploymentConfig) error {
	// This will fail if the DC is already paused because of whatever reason
	name := c.obj.GetName()
	ns := c.obj.GetNamespace()
	glog.V(3).Infof("Starting (Instantiating) the deploymentconfig %s/%s rollout", ns, name)
	deployRequest := osv1.DeploymentRequest{Name: name, Latest: true, Force: true}
	_, err := c.clients.osClient.AppsV1().DeploymentConfigs(ns).
		Instantiate(context.TODO(), name, &deployRequest, metav1.CreateOptions{})
	if err == nil {
		glog.V(3).Infof("Rolled out (Instantiated) the deploymentconfig %s/%s", ns, name)
	}

	return err
}

func (c *parentController) shouldRolloutDeploymentConfig() (bool, *osv1.DeploymentConfig, error) {
	objName := c.obj.GetNamespace() + "/" + c.obj.GetName()
	objKind := c.obj.GetKind()
	if objKind != util.KindDeploymentConfig {
		return false, nil, nil
	}

	typedDC := osv1.DeploymentConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(c.obj.Object, &typedDC); err != nil {
		return false, nil, fmt.Errorf("error converting unstructured %s %s to typed %s: %v", objKind, objName, objKind, err)
	}

	spec := typedDC.Spec
	// if no triggers/empty triggers are set ||
	// if only imageChange trigger and no other trigger is set
	shouldRollout := (len(spec.Triggers) == 0) ||
		(len(spec.Triggers) == 1 && spec.Triggers[0].Type == osv1.DeploymentTriggerOnImageChange)

	if shouldRollout && spec.Paused {
		glog.Errorf("%s %s needs manual rollout, but is paused. Aborting rollout.", objKind, objName)
		return false, nil, fmt.Errorf("rollout aborted as %s %s needs manual rollout, but is paused", objKind, objName)
	}

	return shouldRollout, &typedDC, nil
}

// Wait for all of the new replicas to be created
func (c *parentController) waitForAllNewReplicasToBeCreated(name string) error {
	return wait.Poll(DefaultRetrySleepInterval, DefaultWaitReplicaToBeScheduled, func() (bool, error) {
		obj, errInternal := c.clients.dynNamespacedClient.Get(context.TODO(), name, metav1.GetOptions{})
		if errInternal != nil {
			return false, errInternal
		}

		var replicas, updatedReplicas int64
		found := false

		if obj.GetKind() == util.KindDaemonSet {
			replicas, found, errInternal = unstructured.NestedInt64(obj.Object, "status", "desiredNumberScheduled")
		} else {
			replicas, found, errInternal = unstructured.NestedInt64(obj.Object, "spec", "replicas")
		}
		if errInternal != nil || !found {
			return false, errInternal
		}

		if obj.GetKind() == util.KindDaemonSet {
			updatedReplicas, found, errInternal = unstructured.NestedInt64(obj.Object, "status", "updatedNumberScheduled")
		} else {
			updatedReplicas, found, errInternal = unstructured.NestedInt64(obj.Object, "status", "updatedReplicas")
		}
		if errInternal != nil || !found {
			return false, errInternal
		}

		if replicas == updatedReplicas {
			return true, nil
		}

		return false, nil
	})
}

// get the latest warning event for a workload controller
func (c *parentController) getLatestWarningEventsSinceUpdate(namespace, name string, start time.Time) (bool, string) {
	// Get events that belong to the given workload controller
	events, err := c.clients.typedClient.CoreV1().Events(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return false, ""
	}
	var latestEnt apicorev1.Event
	visited := make(map[string]bool, 0)
	for _, ent := range events.Items {
		if ent.Type == apicorev1.EventTypeWarning && ent.CreationTimestamp.After(start) && strings.HasPrefix(ent.InvolvedObject.Name, name) {
			if ent.CreationTimestamp.After(latestEnt.CreationTimestamp.Time) {
				latestEnt = ent
			}
			// Log all of non-duplicated warning events
			if visited[ent.Reason] {
				continue
			}
			visited[ent.Reason] = true
			glog.V(2).Infof("Found warning event on the workload controller %s/%s: %+v", namespace, name, ent.Message)

		}
	}
	if !latestEnt.CreationTimestamp.IsZero() {
		return true, latestEnt.Message
	}
	return false, ""
}

// Whether Operator controller should be skipped when executing a resize action on a K8s controller
// based on the label. If the SkipOperatorLabel is set to true on a K8s controller, resize action
// will directly update this controller regardless of upper Operator controller.
func (c *parentController) shouldSkipOperator(controller *unstructured.Unstructured) bool {
	labels := controller.GetLabels()
	if labels == nil {
		return false
	}
	labelVal, exists := labels[actionutil.SkipOperatorLabel]
	if exists && strings.EqualFold(labelVal, "true") {
		glog.Infof("Directly updating '%s %s/%s' regardless of Operator controller because '%s' label is set to true.",
			controller.GetKind(), controller.GetNamespace(), controller.GetName(), actionutil.SkipOperatorLabel)
		return true
	}
	return false
}

func (rc *parentController) String() string {
	return rc.name
}

// Returns a flag indicating if the supplied application matches the selector of a GitOps configuration override
func isAppSelectorMatch(selector string, objName string) bool {
	match := false
	if selector != "" {
		match, _ = regexp.MatchString(selector, objName)
		if match {
			glog.V(2).Infof("Application %v matches GitOps override selector [%v]. Using configuration override.", objName, selector)
		}
	}
	return match
}

// Returns a flag indicating if the supplied application is a member of the GitOps configuration override's whitelist
func isAppInWhitelist(arr []string, objName string) bool {
	withoutNamespace := objName[strings.IndexByte(objName, '/')+1:]
	for _, v := range arr {
		if v == objName || v == withoutNamespace {
			glog.V(2).Infof("Application %v found in GitOps override whitelist. Using configuration override.", objName)
			return true
		}
	}
	return false
}

// Returns a flag indicating if there exists a GitOps configuration override for the supplied application
func isGitOpsConfigOverridden(config *gitopsv1alpha1.Configuration, objName string) bool {
	return isAppSelectorMatch(config.Selector, objName) || isAppInWhitelist(config.Whitelist, objName)
}

// Returns the credentials to be used for a GitOps operation.
func getGitOpsCredentials(overrideConfig *gitopsv1alpha1.Configuration, defaultConfig gitops.GitConfig) (string, string, string, string) {
	if overrideConfig.Credentials.Username != "" {
		glog.V(4).Infof("Overriding GitOps credentials to use username [%v] instead of the default.", overrideConfig.Credentials.Username)
		return overrideConfig.Credentials.Email,
			overrideConfig.Credentials.Username,
			overrideConfig.Credentials.SecretName,
			overrideConfig.Credentials.SecretNamespace
	}
	return defaultConfig.GitEmail,
		defaultConfig.GitUsername,
		defaultConfig.GitSecretName,
		defaultConfig.GitSecretNamespace
}

// Returns the GitOps configuration for the supplied application. If an override exists, it will return it. Otherwise,
// it will return the default configuration supplied that was supplied as a runtime parameter.
func (c *parentController) GetGitOpsConfig(obj *unstructured.Unstructured) gitops.GitConfig {
	appName := c.managerApp.Name
	glog.V(3).Infof("Checking for GitOps configuration override for %v...", appName)
	// Lock the cache to ensure the discovery process doesn't overwrite the configs while processing the overrides
	c.gitOpsConfigCacheLock.Lock()
	defer c.gitOpsConfigCacheLock.Unlock()
	namespaceConfigOverrides := c.gitOpsConfigCache[obj.GetNamespace()]
	for _, configOverride := range namespaceConfigOverrides {
		if isGitOpsConfigOverridden(configOverride, appName) {
			glog.V(3).Infof("Found GitOps configuration override for [%v].", appName)
			email, username, secretName, secretNamespace := getGitOpsCredentials(configOverride, c.gitConfig)
			return gitops.GitConfig{
				GitSecretNamespace: secretNamespace,
				GitSecretName:      secretName,
				GitUsername:        username,
				GitEmail:           email,
				CommitMode:         string(configOverride.CommitMode),
			}
		}
	}
	// No override was found. Return the default config.
	glog.V(3).Infof("No GitOps configuration override found for [%v]. Using default configuration.", appName)
	return c.gitConfig
}

// This gets all the resource paths for the containers from the controller spec for the supported controller types
// resourcePath is similar for Deployment, StatefulSet and DaemonSet - "spec.template.spec.containers[?(@.name==“xxx”)].resources"
func getResourcePath(controllerType string, k8sSpec *k8sControllerSpec) ([]string, error) {
	var resourcePaths []string
	switch controllerType {
	case util.KindDeployment, util.KindDaemonSet, util.KindStatefulSet:
		for _, container := range k8sSpec.podSpec.Containers {
			resourcePath := ".spec.template.spec.containers[?(@.name==" + "\"" + container.Name + "\"" + ")].resources"
			resourcePaths = append(resourcePaths, resourcePath)
		}
		glog.V(4).Infof("found resource paths for supported controller type: %v", controllerType)
		return resourcePaths, nil
		// TODO: we don't support any custom controllers currently, since when building podspec it resolves in unexpected controller type.
		// default resource path for custome custroller - ".spec.containers[?(@.name=="xxxx")].resources"
	}
	return resourcePaths, fmt.Errorf("unsupported controller type: %v", controllerType)
}
