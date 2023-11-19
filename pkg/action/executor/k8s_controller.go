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
	osv1 "github.com/openshift/api/apps/v1"
	osclient "github.com/openshift/client-go/apps/clientset/versioned"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	apicorev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/dynamic"
	typedClient "k8s.io/client-go/kubernetes"

	"github.ibm.com/turbonomic/kubeturbo/pkg/action/executor/gitops"
	actionutil "github.ibm.com/turbonomic/kubeturbo/pkg/action/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	discoveryutil "github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/features"
	"github.ibm.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
	gitopsv1alpha1 "github.ibm.com/turbonomic/turbo-gitops/api/v1alpha1"
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
	actionType            proto.ActionItemDTO_ActionType
}

type pathTemplate string

// The resource paths to the bottom-level controller for different actions
const (
	ControllerHorizontalScalePathTemplate pathTemplate = ".spec.replicas"
	ControllerRightSizePathTemplate       pathTemplate = ".spec.template.spec.containers[?(@.name==" + "\"%v\"" + ")].resources"
)

// A predefined mapping from controller type and action type to the corresponding resource path
type actionTypeToPathTemplate map[proto.ActionItemDTO_ActionType]pathTemplate

var (
	controllerTypeToPathTemplate = map[string]actionTypeToPathTemplate{
		util.KindDeployment: {
			proto.ActionItemDTO_RIGHT_SIZE:       ControllerRightSizePathTemplate,
			proto.ActionItemDTO_HORIZONTAL_SCALE: ControllerHorizontalScalePathTemplate,
		},
		util.KindDaemonSet: {
			proto.ActionItemDTO_RIGHT_SIZE: ControllerRightSizePathTemplate,
		},
		util.KindStatefulSet: {
			proto.ActionItemDTO_RIGHT_SIZE:       ControllerRightSizePathTemplate,
			proto.ActionItemDTO_HORIZONTAL_SCALE: ControllerHorizontalScalePathTemplate,
		},
	}
)

func (pc *parentController) get(name string) (*k8sControllerSpec, error) {
	obj, err := pc.clients.dynNamespacedClient.Get(context.TODO(), name, metav1.GetOptions{})
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

	pc.obj = obj
	pc.backupObj = pc.obj.DeepCopy()
	int32Replicas := int32(replicas)
	return &k8sControllerSpec{
		replicas:       &int32Replicas,
		podSpec:        &podSpec,
		controllerName: fmt.Sprintf("%s-%s", kind, objName),
	}, nil
}

func (pc *parentController) update(updatedSpec *k8sControllerSpec) error {
	objName := fmt.Sprintf("%s/%s", pc.obj.GetNamespace(), pc.obj.GetName())
	kind := pc.obj.GetKind()

	replicaVal := int64(*updatedSpec.replicas)
	podSpecUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(updatedSpec.podSpec)
	if err != nil {
		return fmt.Errorf("error converting pod spec to unstructured pod spec for %s %s: %v", kind, objName, err)
	}
	if kind != util.KindDaemonSet { // daemonsets do not have replica field
		if err := unstructured.SetNestedField(pc.obj.Object, replicaVal, "spec", "replicas"); err != nil {
			return fmt.Errorf("error setting replicas into unstructured %s %s: %v", kind, objName, err)
		}
	}
	if err := unstructured.SetNestedField(pc.obj.Object, podSpecUnstructured, "spec", "template", "spec"); err != nil {
		return fmt.Errorf("error setting podSpec into unstructured %s %s: %v", kind, objName, err)
	}

	if pc.managerApp != nil &&
		pc.managerApp.Type != repository.AppTypeK8s &&
		utilfeature.DefaultFeatureGate.Enabled(features.GitopsApps) {
		var manager gitops.GitopsManager
		switch pc.managerApp.Type {
		case repository.AppTypeArgoCD:
			gitOpsConfig := pc.GetGitOpsConfig(pc.obj)
			// The workload is managed by a pipeline controller (argoCD) which replicates
			// it from a source of truth
			manager = gitops.NewGitManager(gitOpsConfig, pc.clients.typedClient,
				pc.clients.dynClient, pc.obj, pc.managerApp, pc.k8sClusterId)
			glog.Infof("Gitops pipeline detected.")
		default:
			return fmt.Errorf("unsupported gitops manager type: %v", pc.managerApp.Type)
		}

		completionFn, completionData, err := manager.Update(int64(*updatedSpec.replicas), podSpecUnstructured)
		if err != nil {
			return fmt.Errorf("failed to update the gitops managed source of truth: %v", err)
		}
		return manager.WaitForActionCompletion(completionFn, completionData)
	}

	ownerInfo, isOwnerSet := discoveryutil.GetOwnerInfo(pc.obj.GetOwnerReferences())
	if !pc.shouldSkipOperator(pc.obj) && isOwnerSet {
		// If k8s controller is controlled by custom controller, update the CR using OperatorResourceMapping
		// if SkipOperatorLabel is not set or not true.
		glog.Infof("Updating %v %v via operator for %v action ...", kind, objName, pc.actionType)
		if pc.ormClient == nil {
			return fmt.Errorf("failed to execute action with nil ORMClient")
		}
		ownedResourcePaths, err := pc.getOwnedResourcePaths(kind, updatedSpec)
		if err != nil {
			return fmt.Errorf("unable to get resource paths: %v", err)
		}
		glog.V(2).Infof("Detected owned resource paths %v for action %v.",
			strings.Join(ownedResourcePaths, ", "), pc.actionType)
		ownerResources, err := pc.ormClient.GetOwnerResourcesForOwnedResources(pc.obj, ownerInfo, ownedResourcePaths)
		if err != nil {
			return fmt.Errorf("unable to get owner resources: %v", err)
		}
		glog.V(2).Info("Detected top level owner resources.")
		return pc.ormClient.UpdateOwners(pc.obj, ownerInfo, ownerResources)
	} else {
		start := time.Now()
		_, err = pc.clients.dynNamespacedClient.Update(context.TODO(), pc.obj, metav1.UpdateOptions{})
		if err != nil {
			return err
		}

		if utilfeature.DefaultFeatureGate.Enabled(features.ForceDeploymentConfigRollout) {
			needsRollout, _, err := pc.shouldRolloutDeploymentConfig()
			if err != nil {
				return err
			}
			if needsRollout {
				err = pc.rolloutDeploymentConfig()
				if err != nil {
					return err
				}
			}
		}

		if utilfeature.DefaultFeatureGate.Enabled(features.AllowIncreaseNsQuota4Resizing) {
			err4Waiting := pc.waitForAllNewReplicasToBeCreated(pc.obj.GetName(), start)
			if err4Waiting != nil {
				glog.V(2).Infof("Get error while waiting for the new replicas to be created for the workload controller %s/%s:%v", pc.obj.GetNamespace(), pc.obj.GetName(), err4Waiting)
				if strings.Contains(err4Waiting.Error(), "forbidden") {
					return util.NewSkipRetryError(err4Waiting.Error()) // this will skip the retry in updateWithRetry
				}
				if hasWarningEvent, warningInfo := pc.getLatestWarningEventsSinceUpdate(pc.obj.GetNamespace(), pc.obj.GetName(), start); hasWarningEvent {
					return fmt.Errorf(warningInfo)
				}
				return err4Waiting
			}
			glog.V(2).Infof("All of the new replicasets get created for the workload controller %s/%s", pc.obj.GetNamespace(), pc.obj.GetName())
		}
	}
	return err
}

func (pc *parentController) revert() error {
	oldPodSpecUnstructured, found, err := unstructured.NestedFieldCopy(pc.backupObj.Object, "spec", "template", "spec")
	if err != nil || !found {
		return err
	}
	currentObj, err := pc.clients.dynNamespacedClient.Get(context.TODO(), pc.obj.GetName(), metav1.GetOptions{})
	if err != nil {
		return err
	}
	currentPodSpecUnstructured, found, err := unstructured.NestedFieldCopy(currentObj.Object, "spec", "template", "spec")
	if err != nil || !found {
		return err
	}
	if reflect.DeepEqual(oldPodSpecUnstructured, currentPodSpecUnstructured) {
		glog.V(4).Infof("There is no change between the original pod spec and the current one for the workload controller %s/%s, no need to revert", pc.obj.GetNamespace(), pc.obj.GetName())
		return nil
	}
	if err := unstructured.SetNestedField(currentObj.Object, oldPodSpecUnstructured, "spec", "template", "spec"); err != nil {
		return err
	}
	if _, err = pc.clients.dynNamespacedClient.Update(context.TODO(), currentObj, metav1.UpdateOptions{}); err != nil {
		return err
	}
	return nil
}

func (pc *parentController) rolloutDeploymentConfig() error {
	// This will fail if the DC is already paused because of whatever reason
	name := pc.obj.GetName()
	ns := pc.obj.GetNamespace()
	glog.V(3).Infof("Starting (Instantiating) the deploymentconfig %s/%s rollout", ns, name)
	deployRequest := osv1.DeploymentRequest{Name: name, Latest: true, Force: true}
	_, err := pc.clients.osClient.AppsV1().DeploymentConfigs(ns).
		Instantiate(context.TODO(), name, &deployRequest, metav1.CreateOptions{})
	if err == nil {
		glog.V(3).Infof("Rolled out (Instantiated) the deploymentconfig %s/%s", ns, name)
	}

	return err
}

func (pc *parentController) shouldRolloutDeploymentConfig() (bool, *osv1.DeploymentConfig, error) {
	objName := pc.obj.GetNamespace() + "/" + pc.obj.GetName()
	objKind := pc.obj.GetKind()
	if objKind != util.KindDeploymentConfig {
		return false, nil, nil
	}

	typedDC := osv1.DeploymentConfig{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(pc.obj.Object, &typedDC); err != nil {
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

// Wait for all the new replicas to be created
func (pc *parentController) waitForAllNewReplicasToBeCreated(name string, start time.Time) error {
	return wait.Poll(DefaultRetrySleepInterval, DefaultWaitReplicaToBeScheduled, func() (bool, error) {
		obj, errInternal := pc.clients.dynNamespacedClient.Get(context.TODO(), name, metav1.GetOptions{})
		if errInternal != nil {
			return false, errInternal
		}

		if hasWarningEvent, warningInfo := pc.getLatestWarningEventsSinceUpdate(pc.obj.GetNamespace(), pc.obj.GetName(), start); hasWarningEvent {
			if strings.Contains(warningInfo, "forbidden") {
				// if there is a forbidden error, return an error which will exit the meaningless waiting
				glog.Errorf("Failed to create new replica for the workload controller %s/%s as there is a forbidden error: %s", pc.obj.GetNamespace(), pc.obj.GetName(), warningInfo)
				return false, fmt.Errorf(warningInfo)
			}
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
func (pc *parentController) getLatestWarningEventsSinceUpdate(namespace, name string, start time.Time) (bool, string) {
	// Get events that belong to the given workload controller
	events, err := pc.clients.typedClient.CoreV1().Events(namespace).List(context.TODO(), metav1.ListOptions{})
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

// shouldSkipOperator checks whether Operator controller should be skipped when executing an action on a K8s controller
// based on the label. If the SkipOperatorLabel is set to true on a K8s controller, action will directly update this
// controller regardless of upper Operator controller.
func (pc *parentController) shouldSkipOperator(controller *unstructured.Unstructured) bool {
	labels := controller.GetLabels()
	if labels == nil {
		return false
	}
	labelVal, exists := labels[actionutil.SkipOperatorLabel]
	if exists && strings.EqualFold(labelVal, "true") {
		glog.Infof("Directly updating '%s %s/%s' regardless of Operator because '%s' label is set to true.",
			controller.GetKind(), controller.GetNamespace(), controller.GetName(), actionutil.SkipOperatorLabel)
		return true
	}
	return false
}

// getOwnedResourcePaths gets all the resource paths for the containers from the controller spec for the supported
// controller types. The resource paths are predefined based on controller type and action type.
func (pc *parentController) getOwnedResourcePaths(controllerType string, k8sSpec *k8sControllerSpec) ([]string, error) {
	pathTemplatesByActionType, supported := controllerTypeToPathTemplate[controllerType]
	if !supported {
		// TODO: we don't support any custom controllers currently, since when building podspec it resolves
		//   in unexpected controller type.
		return nil, fmt.Errorf("unsupported controller %v", controllerType)
	}
	template, ok := pathTemplatesByActionType[pc.actionType]
	if !ok {
		return nil, fmt.Errorf("unsupported action %v for controller %v", pc.actionType, controllerType)
	}
	if pc.actionType == proto.ActionItemDTO_RIGHT_SIZE {
		// For right size actions, there could be multiple containers in a podSpec
		var resourcePaths []string
		for _, container := range k8sSpec.podSpec.Containers {
			resourcePath := fmt.Sprintf(string(template), container.Name)
			resourcePaths = append(resourcePaths, resourcePath)
		}
		return resourcePaths, nil
	}
	return []string{string(template)}, nil
}

func (pc *parentController) String() string {
	return pc.name
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

// GetGitOpsConfig returns the GitOps configuration for the supplied application. If an override exists, it will return it. Otherwise,
// it will return the default configuration supplied that was supplied as a runtime parameter.
func (pc *parentController) GetGitOpsConfig(obj *unstructured.Unstructured) gitops.GitConfig {
	appName := pc.managerApp.Name
	glog.V(3).Infof("Checking for GitOps configuration override for %v...", appName)
	// Lock the cache to ensure the discovery process doesn't overwrite the configs while processing the overrides
	pc.gitOpsConfigCacheLock.Lock()
	defer pc.gitOpsConfigCacheLock.Unlock()
	namespaceConfigOverrides := pc.gitOpsConfigCache[obj.GetNamespace()]
	for _, configOverride := range namespaceConfigOverrides {
		if isGitOpsConfigOverridden(configOverride, appName) {
			glog.V(3).Infof("Found GitOps configuration override for [%v].", appName)
			email, username, secretName, secretNamespace := getGitOpsCredentials(configOverride, pc.gitConfig)
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
	return pc.gitConfig
}
