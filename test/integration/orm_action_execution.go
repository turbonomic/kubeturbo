package integration

import (
	"context"
	"strings"

	set "github.com/deckarep/golang-set"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/cmd/kubeturbo/app"
	"github.com/turbonomic/kubeturbo/test/integration/framework"
	v1 "k8s.io/api/core/v1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	kubeclientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

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
	var needRevertCR4NsScope, needRevertCR4ClusterScope bool

	BeforeEach(func() {
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
			kubeletClient := s.CreateKubeletClientOrDie(kubeConfig, kubeClient, "", "busybox", map[string]set.Set{}, true)
			apiExtClient, err := apiextclient.NewForConfig(kubeConfig)
			if err != nil {
				glog.Fatalf("Failed to generate apiExtensions client for kubernetes target: %v", err)
			}
			ormClient := resourcemapping.NewORMClient(dynamicClient, apiExtClient)

			probeConfig := createProbeConfigOrDie(kubeClient, kubeletClient, dynamicClient)

			discoveryClientConfig := discovery.NewDiscoveryConfig(probeConfig, nil, app.DefaultValidationWorkers,
				app.DefaultValidationTimeout, aggregation.DefaultContainerUtilizationDataAggStrategy,
				aggregation.DefaultContainerUsageDataAggStrategy, ormClient, app.DefaultDiscoveryWorkers, app.DefaultDiscoveryTimeoutSec,
				app.DefaultDiscoverySamples, app.DefaultDiscoverySampleIntervalSec)
			actionHandlerConfig := action.NewActionHandlerConfig("", nil, nil,
				cluster.NewClusterScraper(kubeClient, dynamicClient, false, nil, ""),
				[]string{"*"}, ormClient, false, true, 60, gitops.GitConfig{})
			actionHandler = action.NewActionHandler(actionHandlerConfig)

			// Kubernetes Probe Discovery Client
			discoveryClient := discovery.NewK8sDiscoveryClient(discoveryClientConfig)
			_, _, err = discoveryClient.DiscoverWithNewFramework("discovery-integration-test")
			framework.ExpectNoError(err, "Failed completing discovery of test cluster")
		}

	})

	Describe("ORM test for namespace scope", func() {
		It("should match with the new CR", func() {
			// 1. Get the deployment
			dep, err := kubeClient.AppsV1().Deployments(ormtest_namespace).Get(context.TODO(), namespacescope_operand_deployment_name, metav1.GetOptions{})
			framework.ExpectNoError(err, "Error gettting the deployment for ORM test")

			// 2. Get the CR and backup the resource value
			operatorCR, err := dynamicClient.Resource(groupVersionRes4nsscope).Namespace(ormtest_namespace).Get(context.TODO(), namespacescope_operator_cr_name, metav1.GetOptions{})
			framework.ExpectNoError(err, "Error gettting the operator CR")
			origCRValue4NsScope, _, err = util.NestedField(operatorCR, "destPath", namespacescope_operand_path_in_cr)

			// 3. Build and execute resize action on the deployment
			targetSE := newResizeWorkloadControllerTargetSE(dep)
			resizeupAction, desiredPodSpec := newResizeActionExecutionDTO(targetSE, ORM_RESIZE_UP, &dep.Spec.Template.Spec)
			_, err = actionHandler.ExecuteAction(resizeupAction, nil, &mockProgressTrack{})
			if err != nil {
				framework.Failf("Failed to execute resizing action for ORM namespace case")
			}

			// 4. Set the revert flag for afterEach
			needRevertCR4NsScope = true

			// 5. Wait and check the result
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
			framework.ExpectNoError(err, "Error gettting the operator CR")
			origCRValue4ClusterScope, _, err = util.NestedField(operatorCR, "destPath", clusterscope_operand_path_in_cr)

			// 3. Build and execute resize action on the deployment
			targetSE := newResizeWorkloadControllerTargetSE(dep)
			resizeupAction, desiredPodSpec := newResizeActionExecutionDTO(targetSE, ORM_RESIZE_DOWN, &dep.Spec.Template.Spec)
			_, err = actionHandler.ExecuteAction(resizeupAction, nil, &mockProgressTrack{})
			if err != nil {
				framework.Failf("Failed to execute resizing action for ORM cluster case")
			}

			// 4. Set the revert flag for afterEach
			needRevertCR4ClusterScope = true

			// 5. Wait and check the result
			containerIdx, err := findContainerIdxInPodSpecByName(desiredPodSpec, clusterscope_operand_container_name)
			framework.ExpectNoError(err, "Can't find the container in the pod's spec")
			waitForOperatorCRToUpdateResource(dynamicClient, groupVersionRes4clusterscope, v1.NamespaceAll, clusterscope_operator_cr_name, clusterscope_operand_path_in_cr, &desiredPodSpec.Containers[containerIdx].Resources)
		})
	})

	AfterEach(func() {
		if needRevertCR4NsScope {
			err := revertOperatorCR(dynamicClient, groupVersionRes4nsscope, ormtest_namespace, namespacescope_operator_cr_name, namespacescope_operand_path_in_cr, origCRValue4NsScope)
			framework.ExpectNoError(err, "Error reverting the namespace scope operator CR")
		}
		if needRevertCR4ClusterScope {
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
	dynResourceClient := client.Resource(groupVersionRes).Namespace(nsName)
	_, err = dynResourceClient.Update(context.TODO(), operatorCR, metav1.UpdateOptions{})
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
				cpuVal, _ = limVal["cpu"].(string)
				memVal, _ = limVal["memory"].(string)
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
