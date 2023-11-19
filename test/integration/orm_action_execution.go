package integration

import (
	"context"
	"math/rand"
	"strconv"
	"strings"
	"time"

	set "github.com/deckarep/golang-set"
	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/cmd/kubeturbo/app"
	"github.ibm.com/turbonomic/kubeturbo/test/integration/framework"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	kubeclientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo"

	"github.ibm.com/turbonomic/kubeturbo/pkg/action"
	"github.ibm.com/turbonomic/kubeturbo/pkg/action/executor/gitops"
	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/aggregation"
	"github.ibm.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
)

const (
	ormtest_namespace                      = "ibm-common-services"
	namespacescope_operand_deployment_name = "auth-idp"
	namespacescope_operand_container_name  = "icp-audit-service"
	namespacescope_operator_cr_name        = "example-authentication"
	namespacescope_operand_path_in_cr      = ".spec.auditService.resources"
	namespacescope_lease_name              = "orm-tests-lease-" + namespacescope_operator_cr_name

	clusterscope_operand_deployment_name = "cert-manager-cainjector"
	clusterscope_operand_container_name  = "cert-manager-cainjector"
	clusterscope_operator_cr_name        = "default"
	clusterscope_operand_path_in_cr      = ".spec.certManagerCAInjector.resources"
	clusterscope_lease_name              = "orm-tests-lease-" + clusterscope_operator_cr_name
)

var (
	groupVersionRes4nsscope = schema.GroupVersionResource{
		Group:    "operator.ibm.com",
		Version:  "v1alpha1",
		Resource: "authentications",
	}
	groupVersionRes4clusterscope = schema.GroupVersionResource{
		Group:    "operator.ibm.com",
		Version:  "v1alpha1",
		Resource: "certmanagers",
	}
)

var _ = Describe("Action Executor ", func() {
	f := framework.NewTestFramework("orm-action-executor")
	var kubeConfig *restclient.Config
	var actionHandler *action.ActionHandler
	var kubeClient *kubeclientset.Clientset
	var dynamicClient dynamic.Interface
	var origCRValue4NsScope, origCRValue4ClusterScope interface{}

	BeforeEach(func() {
		//Skip("As the OCP48 cluster is hitting some problems, temporally skip all of ORM test!")
		if !framework.TestContext.IsOpenShiftTest {
			Skip("Ignoring the case for ORM test.")
		}
		f.BeforeEach()
		// The following setup is shared across tests here
		if kubeConfig == nil {

			kubeConfig := f.GetKubeConfig()
			kubeClient = f.GetKubeClient("orm-action-executor")

			var err error
			dynamicClient, err = dynamic.NewForConfig(kubeConfig)
			if err != nil {
				framework.Failf("Failed to generate dynamic client for kubernetes test cluster: %v", err)
			}

			s := app.NewVMTServer()
			kubeletClient := s.CreateKubeletClientOrDie(kubeConfig, kubeClient, "", "icr.io/cpopen/turbonomic/cpufreqgetter", map[string]set.Set{}, true)

			runtimeClient, err := runtimeclient.New(kubeConfig, runtimeclient.Options{})
			if err != nil {
				glog.Fatalf("Failed to create controller runtime client: %v.", err)
			}
			ormClient := resourcemapping.NewORMClientManager(dynamicClient, kubeConfig)
			probeConfig := createProbeConfigOrDie(kubeClient, kubeletClient, dynamicClient, runtimeClient)
			// mock targetConfig
			targetConfig := configs.K8sTargetConfig{}
			targetConfig.OpenShiftVersion(kubeClient)
			discoveryClientConfig := discovery.NewDiscoveryConfig(probeConfig, &targetConfig, app.DefaultValidationWorkers,
				app.DefaultValidationTimeout, aggregation.DefaultContainerUtilizationDataAggStrategy,
				aggregation.DefaultContainerUsageDataAggStrategy, ormClient, app.DefaultDiscoveryWorkers, app.DefaultDiscoveryTimeoutSec,
				app.DefaultDiscoverySamples, app.DefaultDiscoverySampleIntervalSec, 0)
			actionHandlerConfig := action.NewActionHandlerConfig("", nil,
				cluster.NewClusterScraper(nil, kubeClient, dynamicClient, nil, nil, nil, ""),
				[]string{"*"}, ormClient, false, true, 60, gitops.GitConfig{}, "test-cluster-id")

			actionHandler = action.NewActionHandler(actionHandlerConfig)

			// Kubernetes Probe Discovery Client
			discoveryClient := discovery.NewK8sDiscoveryClient(discoveryClientConfig)
			// Cache operatorResourceSpecMap in ormClient
			discoveryClient.Config.ORMClientManager.CacheORMSpecMap()
		}

	})

	Describe("ORM test for namespace scope", func() {
		It("should match with the new CR", func() {
			if !framework.TestContext.IsBlockingTest {
				Skip("Ignoring the case for blocking test.")
			}

			run_test := func() {
				// 1. Get the deployment
				dep, err := kubeClient.AppsV1().Deployments(ormtest_namespace).Get(context.TODO(), namespacescope_operand_deployment_name, metav1.GetOptions{})
				framework.ExpectNoError(err, "Error gettting the deployment for ORM test")

				// 2. Get the CR and backup the resource value
				operatorCR, err := dynamicClient.Resource(groupVersionRes4nsscope).Namespace(ormtest_namespace).Get(context.TODO(), namespacescope_operator_cr_name, metav1.GetOptions{})
				framework.ExpectNoError(err, "Error getting the operator CR")
				origCRValue4NsScope, _, err = util.NestedField(operatorCR, "destPath", namespacescope_operand_path_in_cr)
				framework.ExpectNoError(err, "Error getting the original value from the CR")

				// 3. Build and execute resize action on the deployment
				targetSE := newResizeWorkloadControllerTargetSE(dep)
				resizeupAction, desiredPodSpec := newResizeActionExecutionDTO(targetSE, &dep.Spec.Template.Spec, namespacescope_operand_container_name)
				_, err = actionHandler.ExecuteAction(resizeupAction, nil, &mockProgressTrack{})
				if err != nil {
					framework.Failf("Failed to execute resizing action for ORM namespace case")
				}

				// 4. Wait and check the result
				containerIdx, err := findContainerIdxInPodSpecByName(desiredPodSpec, namespacescope_operand_container_name)
				framework.ExpectNoError(err, "Can't find the container in the pod's spec")
				waitForOperatorCRToUpdateResource(dynamicClient, groupVersionRes4nsscope, ormtest_namespace, namespacescope_operator_cr_name, namespacescope_operand_path_in_cr, &desiredPodSpec.Containers[containerIdx].Resources)
			}

			acquireLockAndRunTest(kubeClient, namespacescope_lease_name, run_test)
		})

	})

	Describe("ORM test for cluster scope", func() {
		It("should match with the new CR", func() {
			if !framework.TestContext.IsBlockingTest {
				Skip("Ignoring the case for blocking test.")
			}

			run_test := func() {
				// 1. Get the deployment
				dep, err := kubeClient.AppsV1().Deployments(ormtest_namespace).Get(context.TODO(), clusterscope_operand_deployment_name, metav1.GetOptions{})
				framework.ExpectNoError(err, "Error gettting the deployment for ORM test")

				// 2. Get the CR and backup the resource value
				operatorCR, err := dynamicClient.Resource(groupVersionRes4clusterscope).Namespace(v1.NamespaceAll).Get(context.TODO(), clusterscope_operator_cr_name, metav1.GetOptions{})
				framework.ExpectNoError(err, "Error getting the operator CR")
				origCRValue4ClusterScope, _, err = util.NestedField(operatorCR, "destPath", clusterscope_operand_path_in_cr)
				framework.ExpectNoError(err, "Error getting the original value from the CR")

				// 3. Build and execute resize action on the deployment
				targetSE := newResizeWorkloadControllerTargetSE(dep)
				resizeupAction, desiredPodSpec := newResizeActionExecutionDTO(targetSE, &dep.Spec.Template.Spec, clusterscope_operand_container_name)
				_, err = actionHandler.ExecuteAction(resizeupAction, nil, &mockProgressTrack{})
				if err != nil {
					framework.Failf("Failed to execute resizing action for ORM cluster case")
				}

				// 4. Wait and check the result
				containerIdx, err := findContainerIdxInPodSpecByName(desiredPodSpec, clusterscope_operand_container_name)
				framework.ExpectNoError(err, "Can't find the container in the pod's spec")
				waitForOperatorCRToUpdateResource(dynamicClient, groupVersionRes4clusterscope, v1.NamespaceAll, clusterscope_operator_cr_name, clusterscope_operand_path_in_cr, &desiredPodSpec.Containers[containerIdx].Resources)
			}
			acquireLockAndRunTest(kubeClient, clusterscope_lease_name, run_test)
		})
	})

	AfterEach(func() {
		if origCRValue4NsScope != nil {
			err := revertOperatorCR(dynamicClient, groupVersionRes4nsscope, ormtest_namespace, namespacescope_operator_cr_name, namespacescope_operand_path_in_cr, origCRValue4NsScope)
			framework.ExpectNoError(err, "Error reverting the namespace scope operator CR")
		}
		if origCRValue4ClusterScope != nil {
			err := revertOperatorCR(dynamicClient, groupVersionRes4clusterscope, v1.NamespaceAll, clusterscope_operator_cr_name, clusterscope_operand_path_in_cr, origCRValue4ClusterScope)
			framework.ExpectNoError(err, "Error reverting the cluster scope operator CR")
		}
	})
})

func revertOperatorCR(client dynamic.Interface, groupVersionRes schema.GroupVersionResource, nsName, crName, destPath string, value interface{}) error {
	operatorCR, err := client.Resource(groupVersionRes).Namespace(nsName).Get(context.TODO(), crName, metav1.GetOptions{})
	framework.ExpectNoError(err, "Error gettting the operator CR")
	fields := strings.Split(destPath, ".")
	err = util.SetNestedField(operatorCR.Object, value, fields[1:]...)
	_, err = client.Resource(groupVersionRes).Namespace(nsName).Update(context.TODO(), operatorCR, metav1.UpdateOptions{})
	return err
}

func waitForOperatorCRToUpdateResource(client dynamic.Interface, groupVersionRes schema.GroupVersionResource, nsName, crName, resPath string, expectedRes *v1.ResourceRequirements) (*unstructured.Unstructured, error) {
	if waitErr := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		operatorCR, err := client.Resource(groupVersionRes).Namespace(nsName).Get(context.TODO(), crName, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("Unexpected error while getting the operator CR: %v", err)
			return false, nil
		}
		var cpuVal, memVal string
		newCRValue, _, err := util.NestedField(operatorCR, "destPath", resPath)
		if resVal, ok := newCRValue.(map[string]interface{}); ok {
			if limVal, ok := resVal["limits"].(map[string]interface{}); ok {
				cpuVal = limVal["cpu"].(string)
				memVal = limVal["memory"].(string)
			}
		}
		cpuQuan, _ := resource.ParseQuantity(cpuVal)
		memQuan, _ := resource.ParseQuantity(memVal)
		if cpuQuan.Equal(expectedRes.Limits["cpu"]) && memQuan.Equal(expectedRes.Limits["memory"]) {
			return true, nil
		}
		return false, nil
	}); waitErr != nil {
		return nil, waitErr
	}
	return nil, nil
}

// Note: all testcases that make use of an existing resource in a shared cluster must wrap the testcase into a function and call
// acquire_lock_and_run_test() to ensure that the resources are not modified by two or more builds/testcases simultaneously.
// Each unique resource should have its own lease and resources shared by multiple tests must use the same lease.
func acquireLockAndRunTest(kubeClient *kubeclientset.Clientset, lease_name string, test_func func()) {
	id := strconv.Itoa(rand.Int())
	klog.Infof("Acquiring lock for lease %v with id %v", lease_name, id)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lock := &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      lease_name,
			Namespace: ormtest_namespace,
		},
		Client: kubeClient.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: id,
		},
	}

	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   60 * time.Second,
		RenewDeadline:   15 * time.Second,
		RetryPeriod:     5 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				klog.Info("lock acquired!")
				test_func()
				klog.Info("test completed. Release lock...")
				cancel()
			},
			OnStoppedLeading: func() {
				klog.Infof("leader lost: %s", id)
			},
			OnNewLeader: func(identity string) {
				// we're notified when new leader elected
				if identity == id {
					// I just got the lock
					return
				}
				klog.Infof("new leader elected: %s", identity)
			},
		},
	})

}
