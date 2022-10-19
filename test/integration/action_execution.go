package integration

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/turbonomic/kubeturbo/test/integration/framework"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	kubeclientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	osv1 "github.com/openshift/api/apps/v1"
	osclient "github.com/openshift/client-go/apps/clientset/versioned"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"

	"github.com/turbonomic/kubeturbo/pkg/action"
	"github.com/turbonomic/kubeturbo/pkg/action/executor"
	"github.com/turbonomic/kubeturbo/pkg/action/executor/gitops"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
)

const (
	openShiftDeployerLabel       = "openshift.io/deployer-pod-for.name"
	cpuIncrement                 = 50
	memoryIncrement              = 75
	cpuDecrement                 = 25
	memoryDecrement              = 10
	injectedSidecarContainerName = "istio-proxy"
)

const (
	RESIZE_UP = iota
	RESIZE_DOWN
)

var _ = Describe("Action Executor ", func() {
	f := framework.NewTestFramework("action-executor")
	var kubeConfig *restclient.Config
	var namespace string
	var actionHandler *action.ActionHandler
	var kubeClient *kubeclientset.Clientset
	var osClient *osclient.Clientset
	var dynamicClient dynamic.Interface

	//AfterSuite(f.AfterEach)
	BeforeEach(func() {
		f.BeforeEach()
		// The following setup is shared across tests here
		if kubeConfig == nil {

			kubeConfig := f.GetKubeConfig()
			kubeClient = f.GetKubeClient("action-executor")

			var err error
			dynamicClient, err = dynamic.NewForConfig(kubeConfig)
			if err != nil {
				framework.Failf("Failed to generate dynamic client for kubernetes test cluster: %v", err)
			}

			osClient, err = osclient.NewForConfig(kubeConfig)
			if err != nil {
				framework.Failf("Failed to generate openshift client for kubernetes test cluster: %v", err)
			}

			actionHandlerConfig := action.NewActionHandlerConfig("", nil, nil,
				cluster.NewClusterScraper(kubeClient, dynamicClient, nil, false, nil, ""),
				[]string{"*"}, nil, false, true, 60, gitops.GitConfig{}, "test-cluster-id")
			actionHandler = action.NewActionHandler(actionHandlerConfig)
		}
		namespace = f.TestNamespaceName()
		f.GenerateCustomImagePullSecret(namespace)
	})

	Describe("executing action move pod", func() {
		It("should result in new pod on target node", func() {
			if framework.TestContext.IsOpenShiftTest {
				Skip("Ignoring pod move case for the deployment against openshift target.")
			}
			dep, err := createDeployResource(kubeClient, depSingleContainerWithResources(namespace, "", 1, false, false, false, ""))
			framework.ExpectNoError(err, "Error creating test resources")

			pod, err := getPodWithNamePrefix(kubeClient, dep.Name, namespace, "")
			framework.ExpectNoError(err, "Error getting deployments pod")
			// This should not happen. We should ideally get a pod.
			if pod == nil {
				framework.Failf("Failed to find a pod for deployment: %s", dep.Name)
			}

			targetNodeName := getTargetSENodeName(f, pod)
			if targetNodeName == "" {
				framework.Failf("Failed to find a pod for deployment: %s", dep.Name)
			}

			_, err = actionHandler.ExecuteAction(newActionExecutionDTO(proto.ActionItemDTO_MOVE,
				newTargetSEFromPod(pod), newHostSEFromNodeName(targetNodeName)), nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Move action failed")

			validateMovedPod(kubeClient, dep.Name, "deployment", namespace, targetNodeName)

		})
	})

	Describe("executing action move pod with volume attached, will also be tested in Istio environment", func() {
		It("should result in new pod on target node", func() {
			if framework.TestContext.IsOpenShiftTest {
				Skip("Ignoring volume based pod move case for the deployment against openshift target.")
			}
			// TODO: The storageclass can be taken as a configurable parameter from commandline
			// This works against a kind cluster. Ensure to update the storageclass name to the right name when
			// running against a different cluster.
			pvc, err := createVolumeClaim(kubeClient, namespace, "standard")
			dep, err := createDeployResource(kubeClient, depSingleContainerWithResources(namespace, pvc.Name, 1, true, false, false, ""))
			framework.ExpectNoError(err, "Error creating test resources")

			pod, err := getPodWithNamePrefix(kubeClient, dep.Name, namespace, "")
			framework.ExpectNoError(err, "Error getting deployments pod")
			// This should not happen. We should ideally get a pod.
			if pod == nil {
				framework.Failf("Failed to find a pod for deployment: %s", dep.Name)
			}

			// Validate if the pod has the injected sidecar container if istio is enabled
			if framework.TestContext.IsIstioEnabled {
				_, err = findContainerIdxInPodSpecByName(&pod.Spec, injectedSidecarContainerName)
				if err != nil {
					framework.Failf("The pod %v isn't injected", podID(pod))
				}
			}

			targetNodeName := getTargetSENodeName(f, pod)
			if targetNodeName == "" {
				framework.Failf("Failed to find a pod for deployment: %s", dep.Name)
			}

			_, err = actionHandler.ExecuteAction(newActionExecutionDTO(proto.ActionItemDTO_MOVE,
				newTargetSEFromPod(pod), newHostSEFromNodeName(targetNodeName)), nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Move action failed")

			validateMovedPod(kubeClient, dep.Name, "deployment", namespace, targetNodeName)

		})
	})

	Describe("executing action move pod on deploymentconfig ", func() {
		It("should result in new pod on target node", func() {
			if !framework.TestContext.IsOpenShiftTest {
				Skip("Ignoring pod move case for the deploymentconfig.")
			}

			// TODO: The storageclass can be taken as a configurable parameter from commandline
			// For now this will need to be updated when running against the given cluster
			dc, err := createDCResource(osClient, genDeploymentConfigWithResources(namespace, "", 1, 1, false))
			framework.ExpectNoError(err, "Error creating test resources")

			pod, err := getDeploymentConfigsPod(kubeClient, dc.Name, namespace, "")
			framework.ExpectNoError(err, "Error getting deployment configs pod")
			// This should not happen. We should ideally get a pod.
			if pod == nil {
				framework.Failf("Failed to find a pod for deployment config: %s", dc.Name)
			}

			targetNodeName := getTargetSENodeName(f, pod)
			if targetNodeName == "" {
				framework.Failf("Failed to find a pod for deployment config: %s", dc.Name)
			}

			_, err = actionHandler.ExecuteAction(newActionExecutionDTO(proto.ActionItemDTO_MOVE,
				newTargetSEFromPod(pod), newHostSEFromNodeName(targetNodeName)), nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Move action failed")

			validateMovedPod(kubeClient, dc.Name, "deploymentconfig", namespace, targetNodeName)

		})
	})

	Describe("executing action move deploymentconfig's pod with volume attached ", func() {
		It("should result in new pod on target node", func() {
			if !framework.TestContext.IsOpenShiftTest {
				Skip("Ignoring volume based pod move case for the deploymentconfig.")
			}

			// TODO: The storageclass can be taken as a configurable parameter from commandline
			// For now this will need to be updated when running against the given cluster
			pvc, err := createVolumeClaim(kubeClient, namespace, "gp2")
			dc, err := createDCResource(osClient, genDeploymentConfigWithResources(namespace, pvc.Name, 1, 1, true))
			framework.ExpectNoError(err, "Error creating test resources")

			pod, err := getDeploymentConfigsPod(kubeClient, dc.Name, namespace, "")
			framework.ExpectNoError(err, "Error getting deployment configs pod")
			// This should not happen. We should ideally get a pod.
			if pod == nil {
				framework.Failf("Failed to find a pod for deployment config: %s", dc.Name)
			}

			targetNodeName := getTargetSENodeName(f, pod)
			if targetNodeName == "" {
				framework.Failf("Failed to find a pod for deployment config: %s", dc.Name)
			}

			_, err = actionHandler.ExecuteAction(newActionExecutionDTO(proto.ActionItemDTO_MOVE,
				newTargetSEFromPod(pod), newHostSEFromNodeName(targetNodeName)), nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Move action failed")

			validateMovedPod(kubeClient, dc.Name, "deploymentconfig", namespace, targetNodeName)

		})
	})

	// Multiple container resize down cpu and memory on resource request/limit for deploymentconfig
	Describe("executing resize action on a deploymentconfig with 2 containers", func() {
		It("should match the expected resource request/limit after resizing on both of cpu and memory", func() {
			if !framework.TestContext.IsOpenShiftTest {
				Skip("Ignoring resize multiple containers for the deploymentconfig.")
			}
			dc, err := createDCResource(osClient, genDeploymentConfigWithResources(namespace, "", 2, 1, false))
			framework.ExpectNoError(err, "Error creating test resources")

			targetSE := newResizeWorkloadControllerTargetSE(dc)
			resizeAction, desiredPodSpec := newResizeActionExecutionDTO(targetSE, &dc.Spec.Template.Spec, "test-cont-1", "test-cont-2")
			_, err = actionHandler.ExecuteAction(resizeAction, nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Resize action on multiple container failed")

			_, err = waitForWorkloadControllerToUpdateResource(osClient, dc, desiredPodSpec)
			if err != nil {
				framework.Failf("Failed to check the change of the resource/request in the new deploymentconfig with multiple containers: %s", err)
			}
		})
	})

	// Test bare pod move
	Describe("executing action move bare pod with volum attached", func() {
		It("should result in new pod on target node", func() {
			if framework.TestContext.IsOpenShiftTest {
				Skip("Ignoring bare pod move against openshift target.")
			}
			pvc, err := createVolumeClaim(kubeClient, namespace, "standard")
			pod, err := createBarePod(kubeClient, genBarePodWithResources(namespace, pvc.Name, 1, true))
			framework.ExpectNoError(err, "Error creating test resources")

			targetNodeName := getTargetSENodeName(f, pod)
			if targetNodeName == "" {
				framework.Failf("Failed to find a new node for the bare pod: %s", pod.Name)
			}

			_, err = actionHandler.ExecuteAction(newActionExecutionDTO(proto.ActionItemDTO_MOVE,
				newTargetSEFromPod(pod), newHostSEFromNodeName(targetNodeName)), nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Bare pod move action failed")

			validateMovedPod(kubeClient, pod.Name, "barepod", namespace, targetNodeName)

		})
	})

	// Single container resize up cpu and memory on resource request/limit
	Describe("executing resize action on a deployment with a single container", func() {
		It("should match the expected resource request/limit after resizing on both of cpu and memory", func() {
			if framework.TestContext.IsOpenShiftTest {
				Skip("Ignoring resize case for the deployment with single container against openshift target.")
			}
			dep, err := createDeployResource(kubeClient, depSingleContainerWithResources(namespace, "", 1, false, false, false, ""))
			framework.ExpectNoError(err, "Error creating test resources")

			targetSE := newResizeWorkloadControllerTargetSE(dep)

			// Resize up cpu and memory
			resizeAction, desiredPodSpec := newResizeActionExecutionDTO(targetSE, &dep.Spec.Template.Spec, "test-cont")
			_, err = actionHandler.ExecuteAction(resizeAction, nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Resize action on limit failed")

			_, err = waitForWorkloadControllerToUpdateResource(kubeClient, dep, desiredPodSpec)
			if err != nil {
				framework.Failf("Failed to check the change of resource/limit in the new deployment: %s", err)
			}
		})
	})

	// Multi container resize down cpu and memory on resource request/limit
	Describe("executing resize action on a deployment with 2 containers, will also be tested in Istio environment", func() {
		It("should match the expected resource request/limit after resizing on both of cpu and memory", func() {
			if framework.TestContext.IsOpenShiftTest {
				Skip("Ignoring resize case for the deployment with multiple containers against openshift target.")
			}
			dep, err := createDeployResource(kubeClient, depMultiContainerWithResources(namespace, "", 2, 1))
			framework.ExpectNoError(err, "Error creating test resources")

			// Validate if the pod has the injected sidecar container if istio is enabled
			if framework.TestContext.IsIstioEnabled {
				pod, err := getPodWithNamePrefix(kubeClient, dep.Name, namespace, "")
				framework.ExpectNoError(err, "Error getting deployments pod")
				_, err = findContainerIdxInPodSpecByName(&pod.Spec, injectedSidecarContainerName)
				if err != nil {
					framework.Failf("The pod %v isn't injected", podID(pod))
				}
			}

			targetSE := newResizeWorkloadControllerTargetSE(dep)
			resizeAction, desiredPodSpec := newResizeActionExecutionDTO(targetSE, &dep.Spec.Template.Spec, "test-cont-1", "test-cont-2")
			_, err = actionHandler.ExecuteAction(resizeAction, nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Resize action on multiple container failed")

			_, err = waitForWorkloadControllerToUpdateResource(kubeClient, dep, desiredPodSpec)
			if err != nil {
				framework.Failf("Failed to check the change of the resource/request in the new deployment with multiple containers: %s", err)
			}
		})
	})

	// Horizontal scale test
	Describe("Executing horizontal scale action on a deployment", func() {
		It("should match the expected replica number", func() {
			if framework.TestContext.IsOpenShiftTest {
				Skip("Ignoring horizontal scal test against openshift target.")
			}
			// create a deployment with 2 replicas
			dep, err := createDeployResource(kubeClient, depSingleContainerWithResources(namespace, "", 2, false, false, false, ""))
			framework.ExpectNoError(err, "Error creating test resources")

			pod, err := getPodWithNamePrefix(kubeClient, dep.Name, namespace, "")
			framework.ExpectNoError(err, "Error getting deployments pod")
			// This should not happen. We should ideally get a pod.
			if pod == nil {
				framework.Failf("Failed to find a pod for deployment: %s", dep.Name)
			}

			// Test the provision action
			_, err = actionHandler.ExecuteAction(newActionExecutionDTO(proto.ActionItemDTO_PROVISION,
				newTargetSEFromPod(pod), nil), nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Failed to execute provision action")

			// As the current replica is 2, new replica should be 3 after the provision action
			_, err = waitForDeploymentToUpdateReplica(kubeClient, dep.Name, dep.Namespace, 3)
			if err != nil {
				framework.Failf("The replica number is incorrect after executing provision action")
			}

			// Test the suspend action
			_, err = actionHandler.ExecuteAction(newActionExecutionDTO(proto.ActionItemDTO_SUSPEND,
				newTargetSEFromPod(pod), nil), nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Failed to execute suspend action")
			// As the current replica is 3, new replica should be 2 after the suspend action
			_, err = waitForDeploymentToUpdateReplica(kubeClient, dep.Name, dep.Namespace, 2)
			if err != nil {
				framework.Failf("The replica number is incorrect after executing suspend action")
			}
		})
	})

	AfterEach(func() {
		if framework.TestContext.IsOpenShiftTest {
			glog.Infof("AfterEach get called with the NS :%v", namespace)
		}
	})
	// TODO: this particular Describe is currently used as the teardown for this
	// whole test (not the suite).
	// This will work only if run sequentially. Find a better way to do this.
	Describe("test teardown", func() {
		if framework.TestContext.IsOpenShiftTest {
			glog.Infof("Teardown get called with the NS :%v", namespace)
		}
		It(fmt.Sprintf("Deleting framework namespace: %s", namespace), func() {
			f.AfterEach()
		})
	})
})

func createDeployResource(client *kubeclientset.Clientset, dep *appsv1.Deployment) (*appsv1.Deployment, error) {
	newDep, err := createDeployment(client, dep)
	if err != nil {
		return nil, err
	}
	return waitForDeployment(client, newDep.Name, newDep.Namespace)
}

// This can also be bootstrapped from a test resource directory
// which holds yaml files.
func depSingleContainerWithResources(namespace, claimName string, replicas int32, withVolume, withGCLabel, paused bool, nodeName string) *appsv1.Deployment {
	dep := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-",
			Namespace:    namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test-app",
				},
			},
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test-app",
					},
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: []corev1.LocalObjectReference{
						{
							Name: framework.DockerImagePullSecretName,
						},
					},
					Containers: []corev1.Container{
						genContainerSpec("test-cont", "50m", "100Mi", "100m", "200Mi"),
					},
				},
			},
		},
	}

	if withVolume {
		dep.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "pod-move-test",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: claimName,
					},
				},
			},
		}
	}

	if withGCLabel {
		addGCLabelDep(&dep)
	}
	if paused {
		dep.Spec.Paused = true
	}

	if nodeName != "" {
		dep.Spec.Template.Spec.NodeName = nodeName
	}

	return &dep
}

func depMultiContainerWithResources(namespace, claimName string, containerNum, replicas int32) *appsv1.Deployment {
	containerlst := []corev1.Container{}
	for i := 0; i < int(containerNum); i++ {
		containerlst = append(containerlst, genContainerSpec(fmt.Sprintf("test-cont-%d", i+1), "50m", "100Mi", "100m", "200Mi"))
	}
	dep := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-",
			Namespace:    namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test-app",
				},
			},
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test-app",
					},
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: []corev1.LocalObjectReference{
						{
							Name: framework.DockerImagePullSecretName,
						},
					},
					Containers: containerlst,
				},
			},
		},
	}

	return &dep
}

func createBarePod(client *kubeclientset.Clientset, pod *corev1.Pod) (*corev1.Pod, error) {
	var errInternal error
	var barepod *corev1.Pod
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		barepod, errInternal = client.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		if errInternal != nil {
			glog.Errorf("Unexpected error while creating pod: %v", errInternal)
			return false, errInternal
		}
		return true, nil
	}); err != nil {
		return nil, err
	}

	return waitForBarePod(client, barepod.Name, barepod.Namespace)
}

func genBarePodWithResources(namespace, claimName string, replicas int32, withVolume bool) *corev1.Pod {
	barePod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-",
			Namespace:    namespace,
		},

		Spec: corev1.PodSpec{
			ImagePullSecrets: []corev1.LocalObjectReference{
				{
					Name: framework.DockerImagePullSecretName,
				},
			},
			Containers: []corev1.Container{
				genContainerSpec("test-cont", "50m", "100Mi", "100m", "200Mi"),
			},
		},
	}

	if withVolume {
		barePod.Spec.Volumes = []corev1.Volume{
			{
				Name: "pod-move-test",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: claimName,
					},
				},
			},
		}
	}

	return &barePod
}

func rsSingleContainerWithResources(namespace string, replicas int32, withGCLabel, withDummyScheduler bool) *appsv1.ReplicaSet {
	rs := appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-",
			Namespace:    namespace,
		},
		Spec: appsv1.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test-app-rs",
				},
			},
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test-app-rs",
					},
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: []corev1.LocalObjectReference{
						{
							Name: framework.DockerImagePullSecretName,
						},
					},
					Containers: []corev1.Container{
						genContainerSpec("test-cont", "50m", "100Mi", "100m", "200Mi"),
					},
				},
			},
		},
	}

	if withGCLabel {
		addGCLabelRS(&rs)
	}
	if withDummyScheduler {
		rs.Spec.Template.Spec.SchedulerName = "turbo-scheduler"
	}

	return &rs
}

func addGCLabelDep(dep *appsv1.Deployment) {
	labels := dep.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[executor.TurboGCLabelKey] = executor.TurboGCLabelVal

	dep.SetLabels(labels)
}

func addGCLabelRS(rs *appsv1.ReplicaSet) {
	labels := rs.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[executor.TurboGCLabelKey] = executor.TurboGCLabelVal

	rs.SetLabels(labels)
}

func createDCResource(client *osclient.Clientset, dc *osv1.DeploymentConfig) (*osv1.DeploymentConfig, error) {
	newDc, err := createDeploymentConfig(client, dc)
	if err != nil {
		return nil, err
	}
	return waitForDeploymentConfig(client, newDc.Name, newDc.Namespace)
}

func genDeploymentConfigWithResources(namespace, claimName string, containerNum, replicas int32, withVolume bool) *osv1.DeploymentConfig {
	containerlst := []corev1.Container{}
	for i := 0; i < int(containerNum); i++ {
		containerlst = append(containerlst, genContainerSpec(fmt.Sprintf("test-cont-%d", i+1), "50m", "100Mi", "100m", "200Mi"))
	}
	dc := osv1.DeploymentConfig{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-",
			Namespace:    namespace,
		},
		Spec: osv1.DeploymentConfigSpec{
			Selector: map[string]string{
				"app": "test-app",
			},
			Replicas: replicas,
			Template: &corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test-app",
					},
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: []corev1.LocalObjectReference{
						{
							Name: framework.DockerImagePullSecretName,
						},
					},
					Containers: containerlst,
				},
			},
		},
	}

	if withVolume {
		dc.Spec.Template.Spec.Volumes = []corev1.Volume{
			{
				Name: "pod-move-test",
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: claimName,
					},
				},
			},
		}
	}

	return &dc
}

func genContainerSpec(name, cpuRequest, memRequest, cpuLimit, memLimit string) corev1.Container {
	return corev1.Container{
		Name:    name,
		Image:   "busybox",
		Command: []string{"/bin/sh"},
		Args:    []string{"-c", "while true; do sleep 30; done;"},
		Resources: corev1.ResourceRequirements{
			Requests: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse(cpuRequest),
				corev1.ResourceMemory: resource.MustParse(memRequest),
			},
			Limits: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse(cpuLimit),
				corev1.ResourceMemory: resource.MustParse(memLimit),
			},
		},
	}
}

func createVolumeClaim(client kubeclientset.Interface, namespace, storageClassName string) (*corev1.PersistentVolumeClaim, error) {
	quantity, _ := resource.ParseQuantity("1Gi")
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "pod-move-test-",
			Namespace:    namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClassName,
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: quantity,
				},
			},
		},
	}

	var newPvc *corev1.PersistentVolumeClaim
	var errInternal error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		newPvc, errInternal = client.CoreV1().PersistentVolumeClaims(namespace).Create(context.TODO(), pvc, metav1.CreateOptions{})
		if errInternal != nil {
			glog.Errorf("Unexpected error while creating PVC for test: %v", errInternal)
			return false, errInternal
		}
		return true, nil
	}); err != nil {
		return nil, err
	}

	return newPvc, nil

}

func createPV(client kubeclientset.Interface, namespace, storageClassName string) (*corev1.PersistentVolume, error) {
	quantity, _ := resource.ParseQuantity("5Gi")
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-pv-",
			Namespace:    namespace,
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: quantity,
			},
			//VolumeMode: Default is FileSystem
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				Local: &corev1.LocalVolumeSource{
					Path: "/opt",
				},
			},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
			StorageClassName:              storageClassName,
			NodeAffinity: &corev1.VolumeNodeAffinity{
				Required: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{

									Key:      "foo",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"bar"},
								},
							},
						},
					},
				},
			},
		},
	}

	var newPV *corev1.PersistentVolume
	var errInternal error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		newPV, errInternal = client.CoreV1().PersistentVolumes().Create(context.TODO(), pv, metav1.CreateOptions{})
		if errInternal != nil {
			glog.Errorf("Unexpected error while creating PV for test: %v", errInternal)
			return false, errInternal
		}
		return true, nil
	}); err != nil {
		return nil, err
	}
	return newPV, nil
}

func createDeployment(client kubeclientset.Interface, dep *appsv1.Deployment) (*appsv1.Deployment, error) {
	var newDep *appsv1.Deployment
	var errInternal error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		newDep, errInternal = client.AppsV1().Deployments(dep.Namespace).Create(context.TODO(), dep, metav1.CreateOptions{})
		if errInternal != nil {
			glog.Errorf("Unexpected error while creating deployment: %v", errInternal)
			return false, errInternal
		}
		return true, nil
	}); err != nil {
		return nil, err
	}
	return newDep, nil
}

func createReplicaSet(client kubeclientset.Interface, rs *appsv1.ReplicaSet) (*appsv1.ReplicaSet, error) {
	var newRS *appsv1.ReplicaSet
	var errInternal error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		newRS, errInternal = client.AppsV1().ReplicaSets(rs.Namespace).Create(context.TODO(), rs, metav1.CreateOptions{})
		if errInternal != nil {
			glog.Errorf("Unexpected error while creating replicaset: %v", errInternal)
			return false, errInternal
		}
		return true, nil
	}); err != nil {
		return nil, err
	}
	return newRS, nil
}

func createStorageClass(client kubeclientset.Interface) (*storagev1.StorageClass, error) {
	storageC := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "pod-with-pv-affinity-",
		},
		Provisioner: "kubernetes.io/no-provisioner",
	}
	var localStorage *storagev1.StorageClass
	var errInternal error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		localStorage, errInternal = client.StorageV1().StorageClasses().Create(context.TODO(), storageC, metav1.CreateOptions{})
		if errInternal != nil {
			glog.Errorf("Unexpected error while creating PVC for test: %v", errInternal)
			return false, errInternal
		}
		return true, nil
	}); err != nil {
		return nil, err
	}

	return localStorage, nil

}

func waitForDeployment(client kubeclientset.Interface, depName, namespace string) (*appsv1.Deployment, error) {
	var newDep *appsv1.Deployment
	var err error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		newDep, err = client.AppsV1().Deployments(namespace).Get(context.TODO(), depName, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("Unexpected error while getting deployment: %v", err)
			return false, err
		}
		if newDep.Status.AvailableReplicas == *newDep.Spec.Replicas {
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, err
	}
	return newDep, nil
}

func waitForDeploymentToUpdateReplica(client kubeclientset.Interface, depName, namespace string, expectedReplica int32) (*appsv1.Deployment, error) {
	var newDep *appsv1.Deployment
	var err error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		newDep, err = client.AppsV1().Deployments(namespace).Get(context.TODO(), depName, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("Unexpected error while getting deployment: %v", err)
			return false, nil
		}
		if *newDep.Spec.Replicas == expectedReplica {
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, err
	}
	return newDep, nil
}

func waitForWorkloadControllerToUpdateResource(client interface{}, workloadController runtime.Object, desiredPodSpec *corev1.PodSpec) (runtime.Object, error) {
	var newControllerObj runtime.Object
	if waitErr := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		switch workloadController.(type) {
		case *appsv1.Deployment:
			dep := workloadController.(*appsv1.Deployment)
			cli := client.(kubeclientset.Interface)
			newDep, err := cli.AppsV1().Deployments(dep.Namespace).Get(context.TODO(), dep.Name, metav1.GetOptions{})
			newControllerObj = newDep
			if err != nil {
				glog.Errorf("Unexpected error while getting deployment: %v", err)
				return false, err
			}
			if reflect.DeepEqual(&newDep.Spec.Template.Spec, desiredPodSpec) {
				return true, nil
			}
		case *osv1.DeploymentConfig:
			dc := workloadController.(*osv1.DeploymentConfig)
			cli := client.(osclient.Interface)
			newDc, err := cli.AppsV1().DeploymentConfigs(dc.Namespace).Get(context.TODO(), dc.Name, metav1.GetOptions{})
			newControllerObj = newDc
			if err != nil {
				glog.Errorf("Unexpected error while getting deployment: %v", err)
				return false, err
			}
			if reflect.DeepEqual(&newDc.Spec.Template.Spec, desiredPodSpec) {
				return true, nil
			}
		default:
			framework.Errorf("The type <%T> of the workload controller is not supported!", workloadController)
		}

		return false, nil
	}); waitErr != nil {
		return nil, waitErr
	}
	return newControllerObj, nil
}

func waitForBarePod(client kubeclientset.Interface, podName, namespace string) (*corev1.Pod, error) {
	var barePod *corev1.Pod
	var err error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		barePod, err = client.CoreV1().Pods(namespace).Get(context.TODO(), podName, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("Unexpected error while getting pod: %v", err)
			return false, nil
		}
		if barePod.Status.Phase == corev1.PodRunning {
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, err
	}
	return barePod, nil
}

// We create a deployment with only 1 replica, so we should be able to get
// the only pod using the name as prefix
func getPodWithNamePrefix(client kubeclientset.Interface, podPrefix, namespace, targetNodeName string) (*corev1.Pod, error) {
	pods, err := client.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, pod := range pods.Items {
		if strings.HasPrefix(pod.Name, podPrefix) {
			if targetNodeName != "" && targetNodeName != pod.Spec.NodeName {
				continue
			}
			return &pod, nil
		}
	}

	return nil, fmt.Errorf("Can't find the right pod that with the prefix %s running on the node %s", podPrefix, targetNodeName)
}

func createDeploymentConfig(client osclient.Interface, dc *osv1.DeploymentConfig) (*osv1.DeploymentConfig, error) {
	var newDc *osv1.DeploymentConfig
	var errInternal error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		newDc, errInternal = client.AppsV1().DeploymentConfigs(dc.Namespace).Create(context.TODO(), dc, metav1.CreateOptions{})
		if errInternal != nil {
			glog.Errorf("Unexpected error while creating deploymentconfig: %v", errInternal)
			return false, nil
		}
		return true, nil
	}); err != nil {
		return nil, err
	}
	return newDc, nil
}

func waitForDeploymentConfig(client osclient.Interface, dcName, namespace string) (*osv1.DeploymentConfig, error) {
	var newDc *osv1.DeploymentConfig
	var err error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		newDc, err = client.AppsV1().DeploymentConfigs(namespace).Get(context.TODO(), dcName, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("Unexpected error while getting deployment config: %v", err)
			return false, nil
		}
		if newDc.Status.AvailableReplicas == newDc.Spec.Replicas {
			return true, nil
		}
		return false, nil
	}); err != nil {
		return nil, err
	}
	return newDc, nil
}

// We create a deployment Config with only 1 replica, so we should be able to get
// the only pod using the name as prefix
func getDeploymentConfigsPod(client kubeclientset.Interface, dcName, namespace, targetNodeName string) (*corev1.Pod, error) {
	pods, err := client.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, pod := range pods.Items {
		if strings.HasPrefix(pod.Name, dcName) && !isOpenShiftDeployerPod(pod.Labels) {
			if targetNodeName != "" && targetNodeName != pod.Spec.NodeName {
				continue
			}
			return &pod, nil
		}
	}

	return nil, fmt.Errorf("Can't find the right pod that with the prefix %s running on the node %s", dcName, targetNodeName)
}

func isOpenShiftDeployerPod(labels map[string]string) bool {
	for k := range labels {
		if k == openShiftDeployerLabel {
			return true
		}
	}
	return false
}

func validateMovedPod(client kubeclientset.Interface, parentName, parentType, namespace, targetNodeName string) *corev1.Pod {
	var pod *corev1.Pod
	var err error

	if parentType == "deployment" || parentType == "barepod" {
		pod, err = getPodWithNamePrefix(client, parentName, namespace, targetNodeName)
	} else if parentType == "deploymentconfig" {
		pod, err = getDeploymentConfigsPod(client, parentName, namespace, targetNodeName)
	}
	framework.ExpectNoError(err, "Get moved pod failed.")

	if pod.Spec.NodeName != targetNodeName {
		framework.Failf("Pod move failed. Pods node: %s vs target node: %s", pod.Spec.NodeName, targetNodeName)
	}

	return pod
}

type mockProgressTrack struct{}

func (p *mockProgressTrack) UpdateProgress(actionState proto.ActionResponseState, description string, progress int32) {
}

func newResizeActionExecutionDTO(targetSE *proto.EntityDTO, podSpec *corev1.PodSpec, containerNames ...string) (*proto.ActionExecutionDTO, *corev1.PodSpec) {
	dto := &proto.ActionExecutionDTO{}
	actionType := proto.ActionItemDTO_RIGHT_SIZE
	desiredPodSpec := podSpec.DeepCopy()
	for _, containerName := range containerNames {
		currentSE := newContainerEntity(containerName)
		containerIdx, err := findContainerIdxInPodSpecByName(podSpec, containerName)
		framework.ExpectNoError(err, "Can't find the container<%v> in the pod's spec", containerName)

		// Build the action item on limits/cpu
		oldLimCpuCap := podSpec.Containers[containerIdx].Resources.Limits.Cpu().MilliValue()
		newLimCpuCap := oldLimCpuCap + cpuIncrement
		aiOnLimCpu := newActionItemDTO(actionType, proto.CommodityDTO_VCPU, oldLimCpuCap, newLimCpuCap, currentSE, targetSE)

		// Build the action item on limits/memory
		oldLimMemCap := podSpec.Containers[containerIdx].Resources.Limits.Memory().Value() / 1024
		newLimMemCap := oldLimMemCap - memoryDecrement*1024
		aiOnLimMem := newActionItemDTO(actionType, proto.CommodityDTO_VMEM, oldLimMemCap, newLimMemCap, currentSE, targetSE)

		// Build the action item on requests/cpu
		oldReqCpuCap := podSpec.Containers[containerIdx].Resources.Requests.Cpu().MilliValue()
		newReqCpuCap := oldReqCpuCap + cpuIncrement
		aiOnReqCpu := newActionItemDTO(actionType, proto.CommodityDTO_VCPU_REQUEST, oldReqCpuCap, newReqCpuCap, currentSE, targetSE)

		// Build the action item on requests/memory
		oldReqMemCap := podSpec.Containers[containerIdx].Resources.Requests.Memory().Value() / 1024
		newReqMemCap := oldReqMemCap - memoryDecrement*1024
		aiOnReqMem := newActionItemDTO(actionType, proto.CommodityDTO_VMEM_REQUEST, oldReqMemCap, newReqMemCap, currentSE, targetSE)

		dto.ActionItem = append(dto.ActionItem, aiOnLimCpu, aiOnLimMem, aiOnReqCpu, aiOnReqMem)

		// Update the desired podSpec
		updatePodSpec(desiredPodSpec, containerIdx, "limits", "cpu", RESIZE_UP, cpuIncrement)
		updatePodSpec(desiredPodSpec, containerIdx, "limits", "memory", RESIZE_DOWN, memoryDecrement)
		updatePodSpec(desiredPodSpec, containerIdx, "requests", "cpu", RESIZE_UP, cpuIncrement)
		updatePodSpec(desiredPodSpec, containerIdx, "requests", "memory", RESIZE_DOWN, memoryDecrement)
	}
	return dto, desiredPodSpec
}

func newActionItemDTO(actionType proto.ActionItemDTO_ActionType, commType proto.CommodityDTO_CommodityType, oldValue, newValue int64, currentSE, targetSE *proto.EntityDTO) *proto.ActionItemDTO {
	ai := &proto.ActionItemDTO{}
	ai.ActionType = &actionType
	ai.CurrentSE = currentSE
	ai.TargetSE = targetSE
	oldCap := float64(oldValue)
	ai.CurrentComm = &proto.CommodityDTO{
		CommodityType: &commType,
		Capacity:      &oldCap,
	}
	newCap := float64(newValue)
	ai.NewComm = &proto.CommodityDTO{
		CommodityType: &commType,
		Capacity:      &newCap,
	}
	return ai
}

func newActionExecutionDTO(actionType proto.ActionItemDTO_ActionType, targetSE, newHostSE *proto.EntityDTO) *proto.ActionExecutionDTO {
	ai := &proto.ActionItemDTO{}
	ai.TargetSE = targetSE
	ai.NewSE = newHostSE
	ai.ActionType = &actionType
	dto := &proto.ActionExecutionDTO{}
	dto.ActionItem = []*proto.ActionItemDTO{ai}

	return dto
}

func newResizeWorkloadControllerTargetSE(workloadController runtime.Object) *proto.EntityDTO {
	var entityDTOBuilder *sdkbuilder.EntityDTOBuilder
	var name, namespace string
	switch workloadController.(type) {
	case *appsv1.Deployment:
		dep := workloadController.(*appsv1.Deployment)
		name = dep.Name
		namespace = dep.Namespace
		entityDTOBuilder = sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_WORKLOAD_CONTROLLER, string(dep.UID))

		entityDTOBuilder.WorkloadControllerData(&proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_DeploymentData{
				DeploymentData: &proto.EntityDTO_DeploymentData{},
			},
		})
	case *osv1.DeploymentConfig:
		dc := workloadController.(*osv1.DeploymentConfig)
		controllerType := "DeploymentConfig"
		name = dc.Name
		namespace = dc.Namespace
		entityDTOBuilder = sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_WORKLOAD_CONTROLLER, string(dc.UID))
		entityDTOBuilder.DisplayName(dc.Name)

		entityDTOBuilder.WorkloadControllerData(&proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_CustomControllerData{
				CustomControllerData: &proto.EntityDTO_CustomControllerData{
					CustomControllerType: &controllerType,
				},
			},
		})
	default:
		framework.Errorf("The type <%T> of the workload controller is not supported!", workloadController)

	}
	entityDTOBuilder.DisplayName(name)
	entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)
	entityDTOBuilder.WithProperty(property.BuildWorkloadControllerNSProperty(namespace))
	se, err := entityDTOBuilder.Create()
	if err != nil {
		framework.Failf("failed to build WorkloadController[%s] entityDTO: %v", namespace+"/"+name, err)
	}

	return se
}

func newTargetSEFromPod(pod *corev1.Pod) *proto.EntityDTO {
	entityType := proto.EntityDTO_CONTAINER_POD
	podDispName := podID(pod)
	podId := string(pod.UID)

	se := &proto.EntityDTO{}
	se.EntityType = &entityType
	se.DisplayName = &podDispName
	se.Id = &podId

	return se
}

func newHostSEFromNodeName(nodeName string) *proto.EntityDTO {
	entityType := proto.EntityDTO_VIRTUAL_MACHINE
	nodeDispName := nodeName

	se := &proto.EntityDTO{}
	se.EntityType = &entityType
	se.DisplayName = &nodeDispName

	return se
}

func newContainerEntity(name string) *proto.EntityDTO {
	entityType := proto.EntityDTO_CONTAINER_SPEC
	dispName := name

	se := &proto.EntityDTO{}
	se.EntityType = &entityType
	se.DisplayName = &dispName

	return se
}

func getTargetSENodeName(f *framework.TestFramework, pod *corev1.Pod) string {
	for _, nodeName := range f.GetClusterNodes() {
		if nodeName != pod.Spec.NodeName {
			return nodeName
		}
	}

	return ""
}

func podID(pod *corev1.Pod) string {
	return fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
}

func updatePodSpec(podSpec *corev1.PodSpec, containerIdx int, resourceType, resourceName string, resizeType, delta int) {
	switch resourceType {
	case "requests":
		switch resourceName {
		case "cpu":
			deltaQuan := resource.MustParse(fmt.Sprintf("%dm", delta))
			cpuQuan := podSpec.Containers[containerIdx].Resources.Requests.Cpu()
			if resizeType == RESIZE_UP {
				cpuQuan.Add(deltaQuan)
			} else if resizeType == RESIZE_DOWN {
				cpuQuan.Sub(deltaQuan)
			}
			// fill in the `s` field in the Quantity struct,otherwise the reflect.DeepEqual will return false
			_ = cpuQuan.String()
			podSpec.Containers[containerIdx].Resources.Requests["cpu"] = *cpuQuan
		case "memory":
			deltaQuan := resource.MustParse(fmt.Sprintf("%dMi", delta))
			memQuan := podSpec.Containers[containerIdx].Resources.Requests.Memory()
			if resizeType == RESIZE_UP {
				memQuan.Add(deltaQuan)
			} else if resizeType == RESIZE_DOWN {
				memQuan.Sub(deltaQuan)
			}
			_ = memQuan.String()
			podSpec.Containers[containerIdx].Resources.Requests["memory"] = *memQuan
		}
	case "limits":
		switch resourceName {
		case "cpu":
			deltaQuan := resource.MustParse(fmt.Sprintf("%dm", delta))
			cpuQuan := podSpec.Containers[containerIdx].Resources.Limits.Cpu()
			if resizeType == RESIZE_UP {
				cpuQuan.Add(deltaQuan)
			} else if resizeType == RESIZE_DOWN {
				cpuQuan.Sub(deltaQuan)
			}
			_ = cpuQuan.String()
			podSpec.Containers[containerIdx].Resources.Limits["cpu"] = *cpuQuan
		case "memory":
			deltaQuan := resource.MustParse(fmt.Sprintf("%dMi", delta))
			memQuan := podSpec.Containers[containerIdx].Resources.Limits.Memory()
			if resizeType == RESIZE_UP {
				memQuan.Add(deltaQuan)
			} else if resizeType == RESIZE_DOWN {
				memQuan.Sub(deltaQuan)
			}
			_ = memQuan.String()
			podSpec.Containers[containerIdx].Resources.Limits["memory"] = *memQuan
		}
	}

}

func findContainerIdxInPodSpecByName(podSpec *corev1.PodSpec, containerName string) (int, error) {
	if podSpec == nil {
		return -1, fmt.Errorf("podSpec is nil")
	}
	for i, cont := range podSpec.Containers {
		if cont.Name == containerName {
			return i, nil
		}
	}
	return -1, fmt.Errorf("can't find the right container with the name %s", containerName)
}
