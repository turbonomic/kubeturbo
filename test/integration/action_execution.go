package integration

import (
	"context"
	"fmt"
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

	"github.com/turbonomic/kubeturbo/pkg/action"
	"github.com/turbonomic/kubeturbo/pkg/action/executor"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
)

const openShiftDeployerLabel = "openshift.io/deployer-pod-for.name"

var _ = Describe("Action Executor ", func() {
	f := framework.NewTestFramework("action-executor")
	var kubeConfig *restclient.Config
	var namespace string
	var actionHandler *action.ActionHandler
	var kubeClient *kubeclientset.Clientset
	var osClient *osclient.Clientset

	//AfterSuite(f.AfterEach)
	BeforeEach(func() {
		f.BeforeEach()
		// The following setup is shared across tests here
		if kubeConfig == nil {

			kubeConfig := f.GetKubeConfig()
			kubeClient = f.GetKubeClient("action-executor")

			dynamicClient, err := dynamic.NewForConfig(kubeConfig)
			if err != nil {
				framework.Failf("Failed to generate dynamic client for kubernetes test cluster: %v", err)
			}

			osClient, err = osclient.NewForConfig(kubeConfig)
			if err != nil {
				framework.Failf("Failed to generate openshift client for kubernetes test cluster: %v", err)
			}

			cluster.NewClusterScraper(kubeClient, dynamicClient)
			actionHandlerConfig := action.NewActionHandlerConfig("", nil, nil,
				cluster.NewClusterScraper(kubeClient, dynamicClient), []string{"*"}, nil, false)

			actionHandler = action.NewActionHandler(actionHandlerConfig)
		}
		namespace = f.TestNamespaceName()
	})

	Describe("executing action move pod", func() {
		It("should result in new pod on target node", func() {
			dep, err := createDeployResource(kubeClient, depSingleContainerWithResources(namespace, "", 1, false, false, false))
			framework.ExpectNoError(err, "Error creating test resources")

			pod, err := getDeploymentsPod(kubeClient, dep.Name, namespace, "")
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

			pod, err := getDeploymentsPod(kubeClient, dep.Name, namespace, "")
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

// We create a deployment with only 1 replica, so we should be able to get
// the only pod using the name as prefix
func getDeploymentsPod(client kubeclientset.Interface, depName, namespace, targetNodeName string) (*corev1.Pod, error) {
	pods, err := client.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, pod := range pods.Items {
		if strings.HasPrefix(pod.Name, depName) {
			if targetNodeName != "" && targetNodeName != pod.Spec.NodeName {
				continue
			}
			return &pod, nil
		}
	}

	return nil, nil
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

	return nil, nil
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

	if parentType == "deployment" {
		pod, err = getDeploymentsPod(client, parentName, namespace, targetNodeName)
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

func newActionExecutionDTO(actionType proto.ActionItemDTO_ActionType, targetSE, newHostSE *proto.EntityDTO) *proto.ActionExecutionDTO {
	ai := &proto.ActionItemDTO{}
	ai.TargetSE = targetSE
	ai.NewSE = newHostSE
	ai.ActionType = &actionType
	dto := &proto.ActionExecutionDTO{}
	dto.ActionItem = []*proto.ActionItemDTO{ai}

	return dto
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
