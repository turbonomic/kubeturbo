package integration

import (
	"context"
	"fmt"
	"time"

	"github.ibm.com/turbonomic/kubeturbo/test/integration/framework"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	kubeclientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo"
	"github.ibm.com/turbonomic/kubeturbo/pkg/action/executor"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker"
)

var _ = Describe("Garbage pod collector ", func() {
	f := framework.NewTestFramework("garbage-collector")
	var namespace string
	var kubeClient *kubeclientset.Clientset
	var dynamicClient dynamic.Interface
	var kubeConfig *restclient.Config

	origQuotaYaml := `
apiVersion: v1
kind: ResourceQuota
spec:
  hard:
    requests.cpu: 50m
    requests.memory: 100Mi
    limits.cpu: 100m
    limits.memory: 200Mi
    pods: "1"
`

	stretchedQuotaYaml := `
apiVersion: v1
kind: ResourceQuota
metadata:
  generateName: test-quota-
  labels:
    kubeturbo.io: gc
spec:
  hard:
    requests.cpu: 51m
    requests.memory: 101Mi
    limits.cpu: 101m
    limits.memory: 201Mi
    pods: "2"
`

	//AfterSuite(f.AfterEach)
	BeforeEach(func() {
		if framework.TestContext.IsOpenShiftTest {
			Skip("Ignoring all of garbage collection cases against openshift target.")
		}
		var err error
		f.BeforeEach()
		// The following setup is shared across tests here
		if kubeConfig == nil {
			kubeConfig := f.GetKubeConfig()
			kubeClient = f.GetKubeClient("garbage-collector")
			dynamicClient, err = dynamic.NewForConfig(kubeConfig)
			if err != nil {
				framework.Failf("Failed to generate dynamic client for kubernetes test cluster: %v", err)
			}
		}
		namespace = f.TestNamespaceName()
		f.GenerateCustomImagePullSecret(namespace)
	})

	Describe("periodic garbage collection", func() {
		It("should result in proper clean up", func() {
			pod1, err := createPodResource(kubeClient, podSingleContainerWithKubeturboGCLabel(namespace))
			framework.ExpectNoError(err, "Error creating test pod with kubeturbo GC label")
			pod2, err := createPodResource(kubeClient, podSingleContainerWithKubeturboGCLabel(namespace))
			framework.ExpectNoError(err, "Error creating test pod with kubeturbo GC label")
			dep, err := createDeployment(kubeClient, depSingleContainerWithResources(namespace, "", 1, false, true, true, ""))
			framework.ExpectNoError(err, "Error creating deployment with garbage collection label")
			rs, err := createReplicaSet(kubeClient, rsSingleContainerWithResources(namespace, 1, true, true))
			framework.ExpectNoError(err, "Error creating replicaset with dummy scheduler")

			stretchedQuota := quotaFromYaml(stretchedQuotaYaml)
			origQuota := quotaFromYaml(origQuotaYaml)
			encodedOrigQuota, err := executor.EncodeQuota(origQuota)
			framework.ExpectNoError(err, "Error encoding test quota")

			stretchedQuota.Annotations = make(map[string]string)
			stretchedQuota.Annotations[executor.QuotaAnnotationKey] = encodedOrigQuota
			quota := createQuota(kubeClient, namespace, stretchedQuota)

			gCChan := make(chan bool)
			defer close(gCChan)
			worker.NewGarbageCollector(kubeClient, dynamicClient, gCChan, 5, time.Second*1, framework.TestContext.IsOpenShiftTest).StartCleanup()

			By("ensuring leaked pods are cleaned up")
			validatePodsCleaned(kubeClient, []*corev1.Pod{pod1, pod2})

			// TODO: At some point make this test generic for supported parents/grandparents
			By("ensuring leaked unhealthy Replicasets are made healthy")
			validateRSHealthy(kubeClient, rs)

			By("ensuring leaked unhealthy deployments are made healthy")
			validateDepHealthy(kubeClient, dep)

			// update the created quota spec with the orig for comparison
			// after cleanup, this is what the quota should become
			quota.Spec.Hard = origQuota.Spec.Hard
			By("ensuring quotas are reverted to their original spec")
			validateQuotaReverted(kubeClient, quota)
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

func createPodResource(client *kubeclientset.Clientset, pod *corev1.Pod) (*corev1.Pod, error) {
	newPod, err := createPod(client, pod)
	if err != nil {
		return nil, err
	}
	return newPod, nil
	// we don't need to wait for this pod to become ready
}

func createPod(client kubeclientset.Interface, pod *corev1.Pod) (*corev1.Pod, error) {
	var newPod *corev1.Pod
	var errInternal error
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		newPod, errInternal = client.CoreV1().Pods(pod.Namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
		if errInternal != nil {
			glog.Errorf("Unexpected error while creating Pod: %v", errInternal)
			return false, nil
		}
		return true, nil
	}); err != nil {
		return nil, err
	}
	return newPod, nil
}

func podSingleContainerWithKubeturboGCLabel(namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-",
			Namespace:    namespace,
			Labels: map[string]string{
				executor.TurboGCLabelKey: executor.TurboGCLabelVal,
			},
		},
		Spec: corev1.PodSpec{
			ImagePullSecrets: []corev1.LocalObjectReference{
				{
					Name: framework.DockerImagePullSecretName,
				},
			},
			Containers: []corev1.Container{
				{
					Name:    "test-cont",
					Image:   framework.BusyboxImage,
					Command: []string{"/bin/sh"},
					Args:    []string{"-c", "while true; do sleep 30; done;"},
				},
			},
		},
	}
}

func validatePodsCleaned(client kubeclientset.Interface, pods []*corev1.Pod) {
	if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		len := len(pods)
		deletedLen := 0
		for _, pod := range pods {
			_, err := client.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				deletedLen++
			}
		}
		if len == deletedLen {
			return true, nil
		}
		return false, nil
	}); err != nil {
		framework.Failf("Pod cleanup by garbage collector failed. %v", err)
	}
}

func validateRSHealthy(client kubeclientset.Interface, rs *appsv1.ReplicaSet) {
	newRs, err := client.AppsV1().ReplicaSets(rs.Namespace).Get(context.TODO(), rs.Name, metav1.GetOptions{})
	if err != nil {
		framework.ExpectNoError(err)
	}

	validateGCLabelRemovedRS(newRs)
	validateRSSchedulerHealthy(newRs)
}

func validateGCLabelRemovedRS(rs *appsv1.ReplicaSet) {
	_, gcLabelFound := rs.Labels[executor.QuotaAnnotationKey]
	if gcLabelFound {
		framework.Failf("The GC label was not removed on RS %s/%s.", rs.Name, rs.Namespace)
	}
}

func validateRSSchedulerHealthy(rs *appsv1.ReplicaSet) {
	healthyScheduler := rs.Spec.Template.Spec.SchedulerName == "default-scheduler"
	if !healthyScheduler {
		framework.Failf("The RS scheduler name is still not right %s/%s.", rs.Name, rs.Namespace)
	}
}

func validateDepHealthy(client kubeclientset.Interface, dep *appsv1.Deployment) {
	newDep, err := client.AppsV1().Deployments(dep.Namespace).Get(context.TODO(), dep.Name, metav1.GetOptions{})
	if err != nil {
		framework.ExpectNoError(err)
	}

	validateGCLabelRemovedDep(newDep)
	validateDepUnpaused(newDep)

}

func validateGCLabelRemovedDep(dep *appsv1.Deployment) {
	_, gcLabelFound := dep.Labels[executor.QuotaAnnotationKey]
	if gcLabelFound {
		framework.Failf("The GC label was not removed on Deployment %s/%s.", dep.Name, dep.Namespace)
	}
}

func validateDepUnpaused(dep *appsv1.Deployment) {
	pausedDeployment := dep.Spec.Paused == true
	if pausedDeployment {
		framework.Failf("The GC label was not removed on Deployment %s/%s.", dep.Name, dep.Namespace)
	}
}
