package integration

import (
	"context"
	"fmt"
	"time"

	"github.com/turbonomic/kubeturbo/test/integration/framework"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeclientset "k8s.io/client-go/kubernetes"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo"
	"github.com/turbonomic/kubeturbo/pkg/action/executor"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker"
)

var _ = Describe("Garbage pod collector ", func() {
	f := framework.NewTestFramework("garbage-collector")
	var namespace string
	var kubeClient *kubeclientset.Clientset

	//AfterSuite(f.AfterEach)
	BeforeEach(func() {
		f.BeforeEach()
		// The following setup is shared across tests here
		kubeClient = f.GetKubeClient("garbage-collector")
		namespace = f.TestNamespaceName()
	})

	Describe("periodic garbage collection", func() {
		It("should result in leaked pods being cleaned up", func() {
			pod, err := createPodResource(kubeClient, PodSingleContainerWithKubeturboGCLabel(namespace))
			framework.ExpectNoError(err, "Error creating test pod with kubeturbo GC label")

			gCChan := make(chan bool)
			defer close(gCChan)
			worker.NewGarbageCollector(kubeClient, gCChan, 1, time.Second*1).StartCleanup()

			validatePodsCleaned(kubeClient, []*corev1.Pod{pod})
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

func PodSingleContainerWithKubeturboGCLabel(namespace string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-",
			Namespace:    namespace,
			Labels: map[string]string{
				executor.TurboGCLabelKey: executor.TurboGCLabelVal,
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
