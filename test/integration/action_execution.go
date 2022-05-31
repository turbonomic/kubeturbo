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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	openShiftDeployerLabel = "openshift.io/deployer-pod-for.name"
	cpuIncrement           = 50
	memoryIncrement        = 75
	cpuDecrement           = 25
	memoryDecrement        = 50
)

const (
	REQUEST_SINGLE_CONTAINER = iota
	LIMIT_SINGLE_CONTAINER
	REQLIM_MULTI_CONTAINER
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
				cluster.NewClusterScraper(kubeClient, dynamicClient, false, nil, ""),
				[]string{"*"}, nil, false, true, 60, gitops.GitConfig{})
			actionHandler = action.NewActionHandler(actionHandlerConfig)
		}
		namespace = f.TestNamespaceName()
	})

	Describe("executing action move pod", func() {
		It("should result in new pod on target node", func() {
			dep, err := createDeployResource(kubeClient, depSingleContainerWithResources(namespace, "", 1, false, false, false))
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

	Describe("executing action move pod with volume attached", func() {
		It("should result in new pod on target node", func() {
			// TODO: The storageclass can be taken as a configurable parameter from commandline
			// This works against a kind cluster. Ensure to update the storageclass name to the right name when
			// running against a different cluster.
			pvc, err := createVolumeClaim(kubeClient, namespace, "standard")
			dep, err := createDeployResource(kubeClient, depSingleContainerWithResources(namespace, pvc.Name, 1, true, false, false))
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

	Describe("executing action move pod on deploymentconfig ", func() {
		It("should result in new pod on target node", func() {
			Skip("Ignoring volume based pod move for deploymentconfig. Remove skipping to execute this against an openshift cluster.")

			// TODO: The storageclass can be taken as a configurable parameter from commandline
			// For now this will need to be updated when running against the given cluster
			dc, err := createDCResource(osClient, dCSingleContainerWithResources(namespace, "", 1, false))
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
			Skip("Ignoring volume based pod move for deploymentconfig. Remove skipping to execute this against an openshift cluster.")

			// TODO: The storageclass can be taken as a configurable parameter from commandline
			// For now this will need to be updated when running against the given cluster
			pvc, err := createVolumeClaim(kubeClient, namespace, "gp2")
			dc, err := createDCResource(osClient, dCSingleContainerWithResources(namespace, pvc.Name, 1, true))
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

	// Test bare pod move
	Describe("executing action move bare pod with volum attached", func() {
		It("should result in new pod on target node", func() {
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

			dep, err := createDeployResource(kubeClient, depSingleContainerWithResources(namespace, "", 1, false, false, false))
			framework.ExpectNoError(err, "Error creating test resources")

			targetSE := newResizeWorkloadControllerTargetSE(dep)

			// Resize up cpu and memory on resource/limit
			resizeAction, desiredPodSpec := newResizeActionExecutionDTO(targetSE, LIMIT_SINGLE_CONTAINER, &dep.Spec.Template.Spec)
			_, err = actionHandler.ExecuteAction(resizeAction, nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Resize action on limit failed")

			dep, err = waitForDeploymentToUpdateResource(kubeClient, dep, desiredPodSpec)
			if err != nil {
				framework.Failf("Failed to check the change of resource/limit in the new deployment: %s", err)
			}

			// Resize up cpu and memory on resource/request
			resizeAction, desiredPodSpec = newResizeActionExecutionDTO(targetSE, REQUEST_SINGLE_CONTAINER, &dep.Spec.Template.Spec)
			_, err = actionHandler.ExecuteAction(resizeAction, nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Resize action on request failed")

			_, err = waitForDeploymentToUpdateResource(kubeClient, dep, desiredPodSpec)
			if err != nil {
				framework.Failf("Failed to check the change of the resource/request in the new deployment: %s", err)
			}
		})
	})

	// Multi container resize down cpu and memory on resource request/limit
	Describe("executing resize action on a deployment with 2 containers", func() {
		It("should match the expected resource request/limit after resizing on both of cpu and memory", func() {

			dep, err := createDeployResource(kubeClient, depMultiContainerWithResources(namespace, "", 1))
			framework.ExpectNoError(err, "Error creating test resources")

			targetSE := newResizeWorkloadControllerTargetSE(dep)
			resizeAction, desiredPodSpec := newResizeActionExecutionDTO(targetSE, REQLIM_MULTI_CONTAINER, &dep.Spec.Template.Spec)
			_, err = actionHandler.ExecuteAction(resizeAction, nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Resize action on multiple container failed")

			_, err = waitForDeploymentToUpdateResource(kubeClient, dep, desiredPodSpec)
			if err != nil {
				framework.Failf("Failed to check the change of the resource/request in the new deployment with multiple containers: %s", err)
			}
		})
	})

	// Horizontal scale test
	Describe("Executing horizontal scale action on a deployment", func() {
		It("should match the expected replica number", func() {
			// create a deployment with 2 replicas
			dep, err := createDeployResource(kubeClient, depSingleContainerWithResources(namespace, "", 2, false, false, false))
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

	// TODO: this particular Describe is currently used as the teardown for this
	// whole test (not the suite).
	// This will work only if run sequentially. Find a better way to do this.
	Describe("test teardowon", func() {
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
func depSingleContainerWithResources(namespace, claimName string, replicas int32, withVolume, withGCLabel, paused bool) *appsv1.Deployment {
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
					Containers: []corev1.Container{
						{
							Name:    "test-cont",
							Image:   "busybox",
							Command: []string{"/bin/sh"},
							Args:    []string{"-c", "while true; do sleep 30; done;"},
							Resources: corev1.ResourceRequirements{
								Limits: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("200Mi"),
								},
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("100Mi"),
								},
							},
						},
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

	return &dep
}

func depMultiContainerWithResources(namespace, claimName string, replicas int32) *appsv1.Deployment {
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
					Containers: []corev1.Container{
						{
							Name:    "test-cont-1",
							Image:   "busybox",
							Command: []string{"/bin/sh"},
							Args:    []string{"-c", "while true; do sleep 30; done;"},
							Resources: corev1.ResourceRequirements{
								Limits: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("200Mi"),
								},
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("100Mi"),
								},
							},
						},
						{
							Name:    "test-cont-2",
							Image:   "busybox",
							Command: []string{"/bin/sh"},
							Args:    []string{"-c", "while true; do sleep 30; done;"},
							Resources: corev1.ResourceRequirements{
								Limits: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("200Mi"),
								},
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("100Mi"),
								},
							},
						},
					},
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
			Containers: []corev1.Container{
				{
					Name:    "test-cont",
					Image:   "busybox",
					Command: []string{"/bin/sh"},
					Args:    []string{"-c", "while true; do sleep 30; done;"},
					Resources: corev1.ResourceRequirements{
						Limits: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("200Mi"),
						},
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("100Mi"),
						},
					},
				},
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
					Containers: []corev1.Container{
						{
							Name:    "test-cont",
							Image:   "busybox",
							Command: []string{"/bin/sh"},
							Args:    []string{"-c", "while true; do sleep 30; done;"},
						},
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

// This can also be bootstrapped from a test resource directory
// which holds yaml files.
func dCSingleContainerWithResources(namespace, claimName string, replicas int32, withVolume bool) *osv1.DeploymentConfig {
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
					Containers: []corev1.Container{
						{
							Name:    "test-cont",
							Image:   "busybox",
							Command: []string{"/bin/sh"},
							Args:    []string{"-c", "while true; do sleep 30; done;"},
							Resources: corev1.ResourceRequirements{
								Limits: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("200Mi"),
								},
								Requests: map[corev1.ResourceName]resource.Quantity{
									corev1.ResourceCPU:    resource.MustParse("50m"),
									corev1.ResourceMemory: resource.MustParse("100Mi"),
								},
							},
						},
					},
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
			return false, nil
		}
		return true, nil
	}); err != nil {
		return nil, err
	}

	return newPvc, nil

}

func createDeployment(client kubeclientset.Interface, dep *appsv1.Deployment) (*appsv1.Deployment, error) {
	var newDep *appsv1.Deployment
	var errInternal error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		newDep, errInternal = client.AppsV1().Deployments(dep.Namespace).Create(context.TODO(), dep, metav1.CreateOptions{})
		if errInternal != nil {
			glog.Errorf("Unexpected error while creating deployment: %v", errInternal)
			return false, nil
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
			return false, nil
		}
		return true, nil
	}); err != nil {
		return nil, err
	}
	return newRS, nil
}

func waitForDeployment(client kubeclientset.Interface, depName, namespace string) (*appsv1.Deployment, error) {
	var newDep *appsv1.Deployment
	var err error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		newDep, err = client.AppsV1().Deployments(namespace).Get(context.TODO(), depName, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("Unexpected error while getting deployment: %v", err)
			return false, nil
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

func waitForDeploymentToUpdateResource(client kubeclientset.Interface, dep *appsv1.Deployment, desiredPodSpec *corev1.PodSpec) (*appsv1.Deployment, error) {
	var newDep *appsv1.Deployment
	var err error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		newDep, err = client.AppsV1().Deployments(dep.Namespace).Get(context.TODO(), dep.Name, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("Unexpected error while getting deployment: %v", err)
			return false, nil
		}

		if reflect.DeepEqual(&newDep.Spec.Template.Spec, desiredPodSpec) {
			return true, nil
		}

		return false, nil
	}); err != nil {
		return nil, err
	}
	return newDep, nil
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
			glog.Errorf("Unexpected error while creating deployment: %v", errInternal)
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

func newResizeActionExecutionDTO(targetSE *proto.EntityDTO, changeType int, podSpec *corev1.PodSpec) (*proto.ActionExecutionDTO, *corev1.PodSpec) {
	dto := &proto.ActionExecutionDTO{}
	actionType := proto.ActionItemDTO_RIGHT_SIZE
	desiredPodSpec := podSpec.DeepCopy()

	switch changeType {
	case REQUEST_SINGLE_CONTAINER:
		currentSE := newContainerEntity("test-cont")

		// Build the action item on cpu
		oldCpuCap := podSpec.Containers[0].Resources.Requests.Cpu().MilliValue()
		newCpuCap := oldCpuCap + cpuIncrement
		aiOnCpu := newActionItemDTO(actionType, proto.CommodityDTO_VCPU_REQUEST, oldCpuCap, newCpuCap, currentSE, targetSE)

		// Build the action item on memory
		oldMemCap := podSpec.Containers[0].Resources.Requests.Memory().Value() / 1024
		newMemCap := oldMemCap + memoryIncrement*1024
		aiOnMem := newActionItemDTO(actionType, proto.CommodityDTO_VMEM_REQUEST, oldMemCap, newMemCap, currentSE, targetSE)

		dto.ActionItem = []*proto.ActionItemDTO{aiOnCpu, aiOnMem}

		// Update the desired podSpec
		updatePodSpec(desiredPodSpec, 0, "requests", "cpu", RESIZE_UP, cpuIncrement)
		updatePodSpec(desiredPodSpec, 0, "requests", "memory", RESIZE_UP, memoryIncrement)
	case LIMIT_SINGLE_CONTAINER:
		currentSE := newContainerEntity("test-cont")

		// Build the action item on cpu
		oldCpuCap := podSpec.Containers[0].Resources.Limits.Cpu().MilliValue()
		newCpuCap := oldCpuCap + cpuIncrement
		aiOnCpu := newActionItemDTO(actionType, proto.CommodityDTO_VCPU, oldCpuCap, newCpuCap, currentSE, targetSE)

		// Build the action item on memory
		oldMemCap := podSpec.Containers[0].Resources.Limits.Memory().Value() / 1024
		newMemCap := oldMemCap + memoryIncrement*1024
		aiOnMem := newActionItemDTO(actionType, proto.CommodityDTO_VMEM, oldMemCap, newMemCap, currentSE, targetSE)

		dto.ActionItem = []*proto.ActionItemDTO{aiOnCpu, aiOnMem}

		// Update the desired podSpec
		updatePodSpec(desiredPodSpec, 0, "limits", "cpu", RESIZE_UP, cpuIncrement)
		updatePodSpec(desiredPodSpec, 0, "limits", "memory", RESIZE_UP, memoryIncrement)
	case REQLIM_MULTI_CONTAINER:
		containerSE1 := newContainerEntity("test-cont-1")
		// Build the resize up action item on request/cpu and resize down action on request/memory for the first container
		oldCpuCap1 := podSpec.Containers[0].Resources.Requests.Cpu().MilliValue()
		newCpuCap1 := oldCpuCap1 + cpuIncrement
		aiOnCpu1 := newActionItemDTO(actionType, proto.CommodityDTO_VCPU_REQUEST, oldCpuCap1, newCpuCap1, containerSE1, targetSE)
		oldMemCap1 := podSpec.Containers[0].Resources.Requests.Memory().Value() / 1024
		newMemCap1 := oldMemCap1 - memoryDecrement*1024
		aiOnMem1 := newActionItemDTO(actionType, proto.CommodityDTO_VMEM_REQUEST, oldMemCap1, newMemCap1, containerSE1, targetSE)

		// Build the resize down action item on limit/cpu and resize up action on limit/memory for the second container
		containerSE2 := newContainerEntity("test-cont-2")
		oldCpuCap2 := podSpec.Containers[1].Resources.Limits.Cpu().MilliValue()
		newCpuCap2 := oldCpuCap2 - cpuDecrement
		aiOnCpu2 := newActionItemDTO(actionType, proto.CommodityDTO_VCPU, oldCpuCap2, newCpuCap2, containerSE2, targetSE)
		oldMemCap2 := podSpec.Containers[1].Resources.Limits.Memory().Value() / 1024
		newMemCap2 := oldMemCap2 + memoryIncrement*1024
		aiOnMem2 := newActionItemDTO(actionType, proto.CommodityDTO_VMEM, oldMemCap2, newMemCap2, containerSE2, targetSE)

		dto.ActionItem = []*proto.ActionItemDTO{aiOnCpu1, aiOnMem1, aiOnCpu2, aiOnMem2}

		// Update the desired podSpec
		updatePodSpec(desiredPodSpec, 0, "requests", "cpu", RESIZE_UP, cpuIncrement)
		updatePodSpec(desiredPodSpec, 0, "requests", "memory", RESIZE_DOWN, memoryDecrement)
		updatePodSpec(desiredPodSpec, 1, "limits", "cpu", RESIZE_DOWN, cpuDecrement)
		updatePodSpec(desiredPodSpec, 1, "limits", "memory", RESIZE_UP, memoryIncrement)
	default:
		framework.Errorf("The change type<%d> isn't supported", changeType)
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

func newResizeWorkloadControllerTargetSE(dep *appsv1.Deployment) *proto.EntityDTO {
	entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_WORKLOAD_CONTROLLER, string(dep.UID))
	entityDTOBuilder.DisplayName(dep.Name)

	entityDTOBuilder.WorkloadControllerData(&proto.EntityDTO_WorkloadControllerData{
		ControllerType: &proto.EntityDTO_WorkloadControllerData_DeploymentData{
			DeploymentData: &proto.EntityDTO_DeploymentData{},
		},
	})
	entityDTOBuilder.WithPowerState(proto.EntityDTO_POWERED_ON)
	entityDTOBuilder.WithProperty(property.BuildWorkloadControllerNSProperty(dep.Namespace))

	se, err := entityDTOBuilder.Create()
	if err != nil {
		framework.Failf("failed to build WorkloadController[%s] entityDTO: %v", dep.Name, err)
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
