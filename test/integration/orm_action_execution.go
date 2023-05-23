package integration

import (
	"context"
	"strings"

	set "github.com/deckarep/golang-set"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/cmd/kubeturbo/app"
	"github.com/turbonomic/kubeturbo/test/integration/framework"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	kubeclientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo"

	"github.com/turbonomic/kubeturbo/pkg/action"
	"github.com/turbonomic/kubeturbo/pkg/action/executor/gitops"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker/aggregation"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"github.com/turbonomic/kubeturbo/pkg/util"
)

const (
	ormtest_namespace                      = "ibm-common-services"
	namespacescope_operand_deployment_name = "auth-idp"
	namespacescope_operand_container_name  = "icp-audit-service"
	namespacescope_operator_cr_name        = "example-authentication"
	namespacescope_operand_path_in_cr      = ".spec.auditService.resources"

	clusterscope_operand_deployment_name = "cert-manager-cainjector"
	clusterscope_operand_container_name  = "cert-manager-cainjector"
	clusterscope_operator_cr_name        = "default"
	clusterscope_operand_path_in_cr      = ".spec.certManagerCAInjector.resources"
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

			discoveryClientConfig := discovery.NewDiscoveryConfig(probeConfig, nil, app.DefaultValidationWorkers,
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

		})

	})

	Describe("ORM test for cluster scope", func() {
		It("should match with the new CR", func() {
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
