package integration

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"strings"

	set "github.com/deckarep/golang-set"
	"github.com/golang/glog"
	. "github.com/onsi/ginkgo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	kubeclientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/turbonomic/kubeturbo/cmd/kubeturbo/app"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery"
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/kubelet"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/master"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker/aggregation"
	kubeletclient "github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"github.com/turbonomic/kubeturbo/test/integration/framework"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
)

/*
	TODO Items: Couple of them which will be very useful additions
	1a. Add support, using a flag to discover a cluster and its entities via kube client,
	calculate the number of entities and groups that should be the result of kubeturbo
	discovery, then trigger discovery and compare the total results.
	1b. Extension of the previous would be to classify entities as per their types, i.e.
	how many workloads, how many pods, how many containers and so on and then find and compare
	each.

	2a. Add a switch to run this test against a cluster with preexisting entities. Test code
	should be able to scan the cluster and get the resources, classify them and get the count
	for each and the total and then conpare these numbers agains the DTOs discovered from
	kubeturbo discovery.
	2b. The other implementation of this switch is what is partially done in this test,
	where new resources are created on top of preexisting (as in the control plane components
	in this test) but the exact counts are not known firsthand. Test code should be able to
	determine the entity counts independently, then create new resources, trigger kubeturbo
	discovery and then compare results.

	3. Make the test less dependent on preexisting constant numbers (as done in this test),
	rather determine those numbers as part of the test. (Or write other tests which can do that).

	4. Add different workload types, viz statefulsets, replicasets, etc. This will ensure covering
	the code paths which include other workload types.

	5. Add workloads with persistent volumes.

*/

var _ = Describe("Discover Cluster", func() {
	f := framework.NewTestFramework("discovery-test")
	var kubeConfig *restclient.Config
	var namespace string
	var kubeClient *kubeclientset.Clientset
	var discoveryClient *discovery.K8sDiscoveryClient

	BeforeEach(func() {
		if framework.TestContext.IsOpenShiftTest {
			Skip("Ignoring all of discovery cases against openshift target.")
		}
		f.BeforeEach()
		// The following setup is shared across tests here
		if kubeConfig == nil {

			kubeConfig := f.GetKubeConfig()
			kubeClient = f.GetKubeClient("discovery-test")
			dynamicClient, err := dynamic.NewForConfig(kubeConfig)
			if err != nil {
				framework.Failf("Failed to generate dynamic client for kubernetes test cluster: %v", err)
			}

			s := app.NewVMTServer()
			kubeletClient := s.CreateKubeletClientOrDie(kubeConfig, kubeClient, "", "icr.io/cpopen/turbonomic/cpufreqgetter", map[string]set.Set{}, true)

			apiExtClient, err := apiextclient.NewForConfig(kubeConfig)
			if err != nil {
				glog.Fatalf("Failed to generate apiExtensions client for kubernetes target: %v", err)
			}
			runtimeClient, err := runtimeclient.New(kubeConfig, runtimeclient.Options{})
			if err != nil {
				glog.Fatalf("Failed to create controller runtime client: %v.", err)
			}
			ormClient := resourcemapping.NewORMClient(dynamicClient, apiExtClient)
			probeConfig := createProbeConfigOrDie(kubeClient, kubeletClient, dynamicClient, runtimeClient)

			discoveryClientConfig := discovery.NewDiscoveryConfig(probeConfig, nil, app.DefaultValidationWorkers,
				app.DefaultValidationTimeout, aggregation.DefaultContainerUtilizationDataAggStrategy,
				aggregation.DefaultContainerUsageDataAggStrategy, ormClient, app.DefaultDiscoveryWorkers, app.DefaultDiscoveryTimeoutSec,
				app.DefaultDiscoverySamples, app.DefaultDiscoverySampleIntervalSec, 0)

			// Kubernetes Probe Discovery Client
			discoveryClient = discovery.NewK8sDiscoveryClient(discoveryClientConfig)
		}
		if namespace == "" {
			namespace = f.TestNamespaceName()
			f.GenerateCustomImagePullSecret(namespace)
		}
	})

	Describe("discovering with discovery framework", func() {
		It("should result in matching set of dtos", func() {
			// 3 different deployments with 2 replicas each
			// i.   deployment with a single container
			// ii.  deployment with 2 containers
			// iii. deployment with 2 containers
			// total of 6 replicas and 10 containers
			_, err := createResourcesForDiscovery(kubeClient, namespace, 2)
			framework.ExpectNoError(err, "Failed creating test resources")

			entityDTOs, _, err := discoveryClient.DiscoverWithNewFramework("discovery-integration-test")
			framework.ExpectNoError(err, "Failed completing discovery of test cluster")

			// We run this against a kind cluster with 1 master and 2 workers created
			// using utility script ./hack/create_kind_cluster.sh
			// The preexisting entities, i.e. control plane components result in a
			// total of 57 entity DTOs and 23 group DTOs.
			// New resources included add up to a total of 91 entities and 33 group DTOs.
			// The entities from other tests might show up in this list.
			// We thus validate that the discovered entities are >= to the numbers here
			//
			// TODO: update this after https://vmturbo.atlassian.net/browse/OM-71015 is complete.
			// Right now the groups won't match the old expected.
			// validateNumbers(entityDTOs, groupDTOs, 91, 33)
			validateThresholds(entityDTOs)
		})

		It("with duplicate node or pod affinity rules should not result in duplicated commodities", func() {
			err := createDeployWithDuplicateAffinities(kubeClient, namespace, 2)
			framework.ExpectNoError(err, "Failed creating test resources")

			entityDTOs, _, err := discoveryClient.DiscoverWithNewFramework("discovery-integration-test")
			framework.ExpectNoError(err, "Failed completing discovery of test cluster")
			validateDuplicateCommodities(entityDTOs)
		})
	})

	Describe("Discovering effects of node with NotReady state", func() {
		var entities []*proto.EntityDTO = nil
		var groups []*proto.GroupDTO = nil
		var notReadyNode *proto.EntityDTO = nil
		testName := "discovery-integration-test"
		nodeName := "kind-worker3"

		It("NotReady node discovery setup", func() {
			// Create a pod to test and attach to the node soon to be in a NotReady state
			_, deployErr := createDeployResource(kubeClient, depSingleContainerWithResources(namespace, "", 1, false, false, false, nodeName))
			framework.ExpectNoError(deployErr, "Error creating test resources")

			// Stop running node worker to simulate node in NotReady state
			_, dockerErr := execute("docker", "stop", nodeName)
			framework.ExpectNoError(dockerErr, "Error running docker stop")

			if nodeStopErr := wait.PollImmediate(framework.PollInterval, framework.DefaultSingleCallTimeout, func() (bool, error) {
				node, errInternal := kubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
				if errInternal != nil {
					glog.Errorf("Unexpected error while finding Node %s: %v", nodeName, errInternal)
					return false, nil
				}

				return !util.NodeIsReady(node), nil
			}); nodeStopErr != nil {
				framework.Failf("Failed to put node in a NotReady state. %v", nodeStopErr)
			}

			entityDTOs, groupDTOs, discoverErr := discoveryClient.DiscoverWithNewFramework(testName)
			if discoverErr != nil {
				framework.Failf(discoverErr.Error())
			}

			entities = entityDTOs
			groups = groupDTOs
		})

		BeforeEach(func() {
			if f.Kubeconfig.CurrentContext != "kind-kind" {
				Skip("Skipping cluster that isn't using context kind-kind")
			}
		})

		It("NotReady node should be detected and it shouldn't have sunspended and provisioned actions", func() {
			notReadyNode = getNotReadyNode(entities)
			if notReadyNode == nil {
				framework.Failf("Node with NotReady state not found")
			}

			if notReadyNode.GetDisplayName() == "" {
				framework.Failf("NotReady nodes should have display names")
			}
			if *notReadyNode.ActionEligibility.Suspendable {
				framework.Failf("NotReady nodes should not have suspended actions")
			}
			if *notReadyNode.ActionEligibility.Cloneable {
				framework.Failf("NotReady nodes should not have provisioned actions")
			}
		})

		It("NotReady nodes should be listed in the NotReady node group", func() {

			notReadyGroup := getGroup(groups, "NotReady-Nodes-")
			if notReadyGroup == nil {
				framework.Failf("Could not find NotReady Node group")
			}

			if notReadyGroup.GetEntityType() != proto.EntityDTO_VIRTUAL_MACHINE {
				framework.Failf("NotReady node group as incorrect entity type")
			}

			if !strings.Contains(notReadyGroup.GetDisplayName(), "NotReady Nodes") {
				framework.Failf("NotReady node group display name has been changed")
			}

			nodeInGroup := false
			for _, uuid := range notReadyGroup.GetMemberList().GetMember() {
				if uuid == notReadyNode.GetId() {
					nodeInGroup = true
				}
			}

			if !nodeInGroup {
				framework.Failf("NotReady node was not found in node group")
			}
		})

		It("Should check that pods on NotReady nodes also have status unknown", func() {
			podsWithNotReadyNode := findEntities(entities, func(entity *proto.EntityDTO) bool {
				return entity.GetEntityType() == proto.EntityDTO_CONTAINER_POD &&
					findOneCommodityBought(entity.CommoditiesBought, func(commBought *proto.EntityDTO_CommodityBought) bool {
						return commBought.GetProviderId() == notReadyNode.GetId()
					}) != nil
			}, len(entities))

			if len(podsWithNotReadyNode) == 0 {
				framework.Failf("NotReady node should have pods")
			}

			for _, pod := range podsWithNotReadyNode {
				if pod.GetPowerState() != proto.EntityDTO_POWERSTATE_UNKNOWN {
					framework.Failf("All pods with NotReady Node provider should be in an unknown state")
				}
			}
		})

		It("Reset kind-worker node", func() {
			// Restart node once finished with the test
			_, dokerStartErr := execute("docker", "start", nodeName)
			framework.ExpectNoError(dokerStartErr, "Error running docker start")
			if nodeStartErr := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
				node, errInternal := kubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
				if errInternal != nil {
					glog.Errorf("Unexpected error while finding Node %s: %v", nodeName, errInternal)
					return false, nil
				}
				return util.NodeIsReady(node), nil
			}); nodeStartErr != nil {
				framework.Failf("Failed to put node in a Ready state. %v", nodeStartErr)
			}
		})
	})

	Describe("discovering pod with pv having affinity rules", func() {
		//Create test resources
		var err error
		var podFullName string
		var nodeName string
		var podNode *corev1.Node
		var podEntityDTO *proto.EntityDTO
		var nodeEntityDTO []*proto.EntityDTO
		var zoneLabelKey = "topology.kubernetes.io/zone"
		var regionLabelKey = "topology.kubernetes.io/region"
		var zoneLabelValue = "us-west-1"
		var regionLabelValue = "us-west-1"
		var commodityZoneValue = zoneLabelKey + "=" + zoneLabelValue
		var commodityRegionValue = regionLabelKey + "=" + regionLabelValue

		It("create test resources", func() {
			//Add Storage Class
			newStorage, err := createStorageClass(kubeClient)
			if newStorage == nil && err != nil {
				framework.Failf("Failed to create Storage Class %s with the error %s", newStorage, err)
			}
			//Add PV with affinity foo=bar
			_, err = createPV(kubeClient, namespace, newStorage.Name)
			if err != nil {
				framework.Failf("Failed to create PV with the error %s", err)
			}
			//Add PVC
			pvc, err := createVolumeClaim(kubeClient, namespace, newStorage.Name)
			if pvc == nil && err != nil {
				framework.Failf("Failed to create PVC %s with the error %s", pvc, err)
			}

			// Update kind-worker node with foo=bar to create node compliant to pv-affinity
			podNode, err = updateNodeWithLabel(kubeClient, "foo", "bar", "kind-worker")
			//Create deployment and validate pod creation
			dep, err := createDeployResource(kubeClient, depSingleContainerWithResources(namespace, pvc.Name, 1, true, false, false, ""))
			framework.ExpectNoError(err, "Error creating test resources")
			pod, err := getPodWithNamePrefix(kubeClient, dep.Name, namespace, "")
			framework.ExpectNoError(err, "Error getting deployments pod")
			// This should not happen. We should ideally get a pod.
			if pod == nil {
				framework.Failf("Failed to find a pod for deployment: %s", dep.Name)
			}

			nodeName = pod.Spec.NodeName
			podFullName = namespace + "/" + pod.Name
		})

		It("discovering pod with pv with affinity rules when feature gate is default(true)", func() {

			//Use Caase 1 : Validate whether VMPM_ACCESS is present in the pod commodity bought list
			//and no OTHER node has VMPM_ACCESS commodity in sold list with feature gate default value

			entityDTOs, _, err := discoveryClient.DiscoverWithNewFramework("discovery-integration-test")
			framework.ExpectNoError(err, "Failed completing discovery of test cluster")
			podEntityDTO, nodeEntityDTO = getPodNodeDTO(entityDTOs, podFullName)

			if !validateBuyerSellerCommodity(podEntityDTO, nodeEntityDTO, nodeName, proto.CommodityDTO_VMPM_ACCESS, "foo in (bar)") {
				framework.Failf("PV affinity is not honored")
			}
		})

		It("pod with pv to honor zone label when feature gate is default(true)", func() {
			//User Case 2 : Test zone label in node and featureGate default value
			podNode, err = updateNodeWithLabel(kubeClient, zoneLabelKey, zoneLabelValue, nodeName)
			podNode, err = updateNodeWithLabel(kubeClient, regionLabelKey, regionLabelValue, nodeName)

			entityDTOs, _, err := discoveryClient.DiscoverWithNewFramework("discovery-integration-test")
			framework.ExpectNoError(err, "Failed completing discovery of test cluster")

			//Verify zone label in the commodity list
			podEntityDTO, nodeEntityDTO = getPodNodeDTO(entityDTOs, podFullName)

			// Verify zone label is honored for pod with pv
			if !validateBuyerSellerCommodity(podEntityDTO, nodeEntityDTO, nodeName, proto.CommodityDTO_LABEL, commodityZoneValue) {
				framework.Failf("Zone label in node for pod with PV is not honored")
			}
			if !validateBuyerSellerCommodity(podEntityDTO, nodeEntityDTO, nodeName, proto.CommodityDTO_LABEL, commodityRegionValue) {
				framework.Failf("Region label in node for pod with PV is not honored")
			}

		})

		It("discovering pod with pv with affinity rules with featureGate disabled", func() {

			// Disable  the feature gate
			honorAzPvFlag := make(map[string]bool)
			honorAzPvFlag["HonorAzLabelPvAffinity"] = false
			err = utilfeature.DefaultMutableFeatureGate.SetFromMap(honorAzPvFlag)
			if err != nil {
				glog.Fatalf("Invalid Feature Gates: %v", err)
			}

			entityDTOs, _, err := discoveryClient.DiscoverWithNewFramework("discovery-integration-test")
			framework.ExpectNoError(err, "Failed completing discovery of test cluster")
			podEntityDTO, nodeEntityDTO = getPodNodeDTO(entityDTOs, podFullName)

			var delNode *corev1.Node = getNodewithNodeName(nodeName, kubeClient)
			if validateBuyerSellerCommodity(podEntityDTO, nodeEntityDTO, nodeName, proto.CommodityDTO_VMPM_ACCESS, "foo in (bar)") {
				podNode = deleteLabelsFromNode(delNode, "foo", kubeClient)
				framework.Failf("PV affinity is not honored")
			}
			//Manual deletion of foo=bar
			podNode = deleteLabelsFromNode(delNode, "foo", kubeClient)
		})

		It("pod with pv to honor zone label when feature gate is disabled", func() {

			//Use Case 3 : Test zone label in node and featureGate value false

			// Disable the feature gate
			honorAzPvFlag := make(map[string]bool)
			honorAzPvFlag["HonorAzLabelPvAffinity"] = false
			err = utilfeature.DefaultMutableFeatureGate.SetFromMap(honorAzPvFlag)
			if err != nil {
				glog.Fatalf("Invalid Feature Gates: %v", err)
			}

			podNode, err = updateNodeWithLabel(kubeClient, zoneLabelKey, zoneLabelValue, nodeName)
			podNode, err = updateNodeWithLabel(kubeClient, regionLabelKey, regionLabelValue, nodeName)

			entityDTOs, _, err := discoveryClient.DiscoverWithNewFramework("discovery-integration-test")
			framework.ExpectNoError(err, "Failed completing discovery of test cluster")

			podEntityDTO, nodeEntityDTO = getPodNodeDTO(entityDTOs, podFullName)

			//Verify zone/region label in the commodity list
			if validateBuyerSellerCommodity(podEntityDTO, nodeEntityDTO, nodeName, proto.CommodityDTO_LABEL, commodityZoneValue) {
				framework.Failf("Zone label in node for pod with PV is considered even if featureGate is disabled")
			}
			if validateBuyerSellerCommodity(podEntityDTO, nodeEntityDTO, nodeName, proto.CommodityDTO_LABEL, commodityRegionValue) {
				framework.Failf("Region label in node for pod with PV is considered even if featureGate is diabledd")
			}

		})
		AfterEach(func() {
			delNode := deleteLabelsFromNode(podNode, zoneLabelKey, kubeClient)
			delNode = deleteLabelsFromNode(delNode, regionLabelKey, kubeClient)
		})
	})

	Describe("test mirror pods", func() {
		testName := "discovery-integration-test"
		mirrorpod_prefix := "static-web"
		var entities []*proto.EntityDTO = nil
		var groups []*proto.GroupDTO = nil
		var delNode *corev1.Node
		var err error

		It("validate dameon settings true for multiple nodepools in a cluster with all nodes havings static pod in the pool", func() {

			// Create AKS and GKE nodepools with one node in each
			_, err = updateNodeWithLabel(kubeClient, util.NodePoolGKE, "MirrorPodTestGKE", "kind-worker2")
			if err != nil {
				framework.Failf("Error in updating the nodepool label")
			}
			_, err = updateNodeWithLabel(kubeClient, util.NodePoolAKS, "MirrorPodTestAKS", "kind-worker3")
			if err != nil {
				framework.Failf("Error in updating the nodepool label")
			}
			entityDTOs, groupDTOs, Err := discoveryClient.DiscoverWithNewFramework(testName)
			if Err != nil {
				framework.Failf(Err.Error())
			}

			for _, entityDTO := range entityDTOs {
				if *entityDTO.EntityType == proto.EntityDTO_CONTAINER_POD &&
					strings.Contains(*entityDTO.DisplayName, mirrorpod_prefix) {
					entities = append(entities, entityDTO)
				}
			}
			groups = groupDTOs
			validateMirrorPods(groups, entities)

		})

		It("validate dameon settings true when all nodes in nodepool has static pod", func() {

			// Create GKE nodepools with two nodes
			entities = nil
			_, err = updateNodeWithLabel(kubeClient, util.NodePoolGKE, "MirrorPodTestGKE", "kind-worker2")
			if err != nil {
				framework.Failf("Error in updating the nodepool label")
			}
			_, err = updateNodeWithLabel(kubeClient, util.NodePoolGKE, "MirrorPodTestGKE", "kind-worker3")
			if err != nil {
				framework.Failf("Error in updating the nodepool label")
			}
			entityDTOs, groupDTOs, Err := discoveryClient.DiscoverWithNewFramework(testName)
			if Err != nil {
				framework.Failf(Err.Error())
			}

			for _, entityDTO := range entityDTOs {
				if *entityDTO.EntityType == proto.EntityDTO_CONTAINER_POD &&
					strings.Contains(*entityDTO.DisplayName, mirrorpod_prefix) {
					entities = append(entities, entityDTO)
				}
			}

			groups = groupDTOs
			//Validate daemon: true for nodepool with 2 nodes
			validateMirrorPods(groups, entities)

		})
		It("validate daemon settings false for a nodepool with not all nodes having staic pods", func() {

			// Create GKE nodepools with two nodes
			entities = nil
			_, err = updateNodeWithLabel(kubeClient, util.NodePoolGKE, "MirrorPodTestGKE", "kind-worker2")
			if err != nil {
				framework.Failf("Error in updating the nodepool label")
			}
			_, err = updateNodeWithLabel(kubeClient, util.NodePoolGKE, "MirrorPodTestGKE", "kind-worker")
			if err != nil {
				framework.Failf("Error in updating the nodepool label")
			}
			entityDTOs, groupDTOs, Err := discoveryClient.DiscoverWithNewFramework(testName)
			if Err != nil {
				framework.Failf(Err.Error())
			}

			for _, entityDTO := range entityDTOs {
				if *entityDTO.EntityType == proto.EntityDTO_CONTAINER_POD &&
					strings.Contains(*entityDTO.DisplayName, mirrorpod_prefix) {
					entities = append(entities, entityDTO)
				}
			}

			groups = groupDTOs

			mirrorPodGroup := getGroup(groups, "Mirror-Pods-")
			var groupMemberList []string = mirrorPodGroup.GetMemberList().GetMember()

			//Group validations
			if mirrorPodGroup == nil {
				framework.Failf("Could not find Mirror Pod group")
			}

			if mirrorPodGroup.GetEntityType() != proto.EntityDTO_CONTAINER_POD {
				framework.Failf("Mirror pod group has incorrect entity type")
			}

			if !strings.Contains(mirrorPodGroup.GetDisplayName(), "Mirror Pods") {
				framework.Failf("Mirror pod group display name has been changed")
			}

			for _, entity := range entities {
				//Validate daemon:false as not all nodes in nodepool have static pods
				if *entity.ConsumerPolicy.Daemon == true {
					framework.Failf("Daemon set for %v mirror pod should be false as not all the nodes in the nodepool has static pod ", entity.DisplayName)
				}
				if !isElementExist(groupMemberList, *entity.Id) {
					framework.Failf("Pod %s is not present in Mirror pod group", *entity.DisplayName)
				}

			}
		})

		AfterEach(func() {
			delNode = getNodewithNodeName("kind-worker2", kubeClient)
			_ = deleteLabelsFromNode(delNode, util.NodePoolGKE, kubeClient)

			delNode = getNodewithNodeName("kind-worker3", kubeClient)
			_ = deleteLabelsFromNode(delNode, util.NodePoolAKS, kubeClient)

			delNode = getNodewithNodeName("kind-worker3", kubeClient)
			_ = deleteLabelsFromNode(delNode, util.NodePoolGKE, kubeClient)

			delNode = getNodewithNodeName("kind-worker", kubeClient)
			_ = deleteLabelsFromNode(delNode, util.NodePoolGKE, kubeClient)

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

func validateMirrorPods(groups []*proto.GroupDTO, entities []*proto.EntityDTO) {
	mirrorPodGroup := getGroup(groups, "Mirror-Pods-")
	var groupMemberList []string = mirrorPodGroup.GetMemberList().GetMember()

	//Group validations

	if mirrorPodGroup == nil {
		framework.Failf("Could not find Mirror Pod group")
	}

	if mirrorPodGroup.GetEntityType() != proto.EntityDTO_CONTAINER_POD {
		framework.Failf("Mirror pod group has incorrect entity type")
	}

	if !strings.Contains(mirrorPodGroup.GetDisplayName(), "Mirror Pods") {
		framework.Failf("Mirror pod group display name has been changed")
	}

	//Validate daemon: true in ConsumerPolicy for pods and group contains the mirror pods
	for _, entity := range entities {
		if *entity.ConsumerPolicy.Daemon != true {
			framework.Failf("Daemon set settings for %v mirror pod is false ", entity.DisplayName)
		}
		if !isElementExist(groupMemberList, *entity.Id) {
			framework.Failf("Pod %s is not present in Mirror pod group", *entity.DisplayName)
		}

	}

}

func isElementExist(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}

func getNodewithNodeName(nodeName string, kubeClient *kubeclientset.Clientset) *corev1.Node {
	var zoneNode *corev1.Node
	zoneNode, err := kubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
	if err != nil {
		framework.Failf("Failed to retrieve node with error %s", err)
	}
	return zoneNode
}

func updateNodeWithLabel(kubeClient *kubeclientset.Clientset, labelKey string,
	labelValue string, nodeName string) (*corev1.Node, error) {

	var nodeToUpdate *corev1.Node
	var err error
	nodeToUpdate = getNodewithNodeName(nodeName, kubeClient)

	nodeToUpdate.Labels[labelKey] = labelValue //Label node w
	nodeToUpdate, err = kubeClient.CoreV1().Nodes().Update(context.TODO(), nodeToUpdate, metav1.UpdateOptions{})
	if err != nil {
		framework.Failf("Failed to update node %s with label: %s %s", nodeToUpdate, labelKey+":"+labelValue, err)
	}
	return nodeToUpdate, err
}

func deleteLabelsFromNode(delNode *corev1.Node, labelKey string, kubeClient *kubeclientset.Clientset) *corev1.Node {
	//Iterate to support case labelKey is not present in the nodelabels
	for label := range delNode.Labels {
		if strings.Contains(label, labelKey) {
			delete(delNode.Labels, label)
		}
	}
	var err error
	podNode, err := kubeClient.CoreV1().Nodes().Update(context.TODO(), delNode, metav1.UpdateOptions{})
	if err != nil {
		framework.Failf("Failed to remove label %s from node %s. Error %s", labelKey, podNode, err)
	}
	return podNode
}

func getPodNodeDTO(entityDTOs []*proto.EntityDTO, podName string) (*proto.EntityDTO, []*proto.EntityDTO) {
	//Identify the pod with pv and all nodes and create respective dto variable and list respectively
	var podEntityDTO *proto.EntityDTO
	var nodeEntityDTO []*proto.EntityDTO

	for _, entityDTO := range entityDTOs {
		if *entityDTO.EntityType == proto.EntityDTO_CONTAINER_POD &&
			*entityDTO.DisplayName == podName {
			podEntityDTO = entityDTO

		} else if *entityDTO.EntityType == proto.EntityDTO_VIRTUAL_MACHINE {
			nodeEntityDTO = append(nodeEntityDTO, entityDTO)
		}
	}
	return podEntityDTO, nodeEntityDTO

}

func validateBuyerSellerCommodity(podEntityDTO *proto.EntityDTO, nodeEntityDTO []*proto.EntityDTO, nodeName string,
	commodityTypeToCheck proto.CommodityDTO_CommodityType, commodityValue string) bool {

	var buyerRegsitered bool
	var sellerRegistered bool

	buyerRegsitered = false
	sellerRegistered = false
	// Validate pod BoughtCommodity list to contain VMPM_ACCESS commodity with key foo in (bar)
	for _, commI := range podEntityDTO.GetCommoditiesBought() {
		if *commI.ProviderType == proto.EntityDTO_VIRTUAL_MACHINE {
			for _, commI := range commI.Bought {
				if *commI.CommodityType == commodityTypeToCheck &&
					strings.Contains(*commI.Key, commodityValue) {
					buyerRegsitered = true
					break
				}
			}
		}
		if buyerRegsitered {
			break
		}
	}
	// Validate commodity is present in no other node
	for _, nDTO := range nodeEntityDTO {
		for _, commI := range nDTO.GetCommoditiesSold() {
			if *commI.CommodityType == commodityTypeToCheck &&
				strings.Contains(*commI.Key, commodityValue) {
				if *nDTO.DisplayName == nodeName { //Make sure no node other node has this commodity
					sellerRegistered = true
				} else {
					framework.Failf(commodityValue+" commodity is present in another node %s", *nDTO.DisplayName)
				}
				break
			}
		}
	}
	// If either of buyer or seller commodity list is not expected return false
	if buyerRegsitered && sellerRegistered {
		return true
	}
	return false
}

func findEntities(vals []*proto.EntityDTO, condition func(v *proto.EntityDTO) bool, howMany int) []*proto.EntityDTO {
	found := []*proto.EntityDTO{}
	for _, v := range vals {
		if howMany == 0 {
			break
		}

		if condition(v) {
			found = append(found, v)
			howMany--
		}
	}
	return found
}

func findGroups(vals []*proto.GroupDTO, condition func(v *proto.GroupDTO) bool, howMany int) []*proto.GroupDTO {
	found := []*proto.GroupDTO{}
	for _, v := range vals {
		if howMany == 0 {
			break
		}

		if condition(v) {
			found = append(found, v)
			howMany--
		}
	}
	return found
}

func findCommoditiesBought(vals []*proto.EntityDTO_CommodityBought, condition func(v *proto.EntityDTO_CommodityBought) bool, howMany int) []*proto.EntityDTO_CommodityBought {
	found := []*proto.EntityDTO_CommodityBought{}
	for _, v := range vals {
		if howMany == 0 {
			break
		}

		if condition(v) {
			found = append(found, v)
			howMany--
		}
	}
	return found
}

func findOneEntity(values []*proto.EntityDTO, condition func(v *proto.EntityDTO) bool) *proto.EntityDTO {
	found := findEntities(values, condition, 1)
	if len(found) == 0 {
		return nil
	}
	return found[0]
}

func findOneGroup(values []*proto.GroupDTO, condition func(v *proto.GroupDTO) bool) *proto.GroupDTO {
	found := findGroups(values, condition, 1)
	if len(found) == 0 {
		return nil
	}
	return found[0]
}

func findOneCommodityBought(values []*proto.EntityDTO_CommodityBought, condition func(v *proto.EntityDTO_CommodityBought) bool) *proto.EntityDTO_CommodityBought {
	found := findCommoditiesBought(values, condition, 1)
	if len(found) == 0 {
		return nil
	}
	return found[0]
}

func execute(name string, arg ...string) (string, error) {
	stdout, err := exec.Command(name, arg...).Output()
	if err != nil {
		glog.Error(err)
	}
	output := string(stdout)
	glog.Info("command output: " + output)
	return output, err
}

func getNotReadyNode(entities []*proto.EntityDTO) *proto.EntityDTO {
	notReadyNode := findOneEntity(entities, func(entity *proto.EntityDTO) bool {
		commBoughtList := entity.GetCommoditiesBought()
		var hasClusterCommodity bool
		for _, commBought := range commBoughtList {
			providerType := commBought.GetProviderType()
			for _, bought := range commBought.GetBought() {
				if bought.GetCommodityType() == proto.CommodityDTO_CLUSTER &&
					providerType == proto.EntityDTO_CONTAINER_PLATFORM_CLUSTER {
					hasClusterCommodity = true
					break
				}
			}
		}
		return entity.GetEntityType() == proto.EntityDTO_VIRTUAL_MACHINE &&
			entity.GetPowerState() == proto.EntityDTO_POWERED_ON &&
			hasClusterCommodity
	})
	if notReadyNode != nil {
		framework.Logf("Successfully found Node with NotReady status.")
	}

	return notReadyNode
}

func getGroup(groups []*proto.GroupDTO, groupName string) *proto.GroupDTO {
	reqGroup := findOneGroup(groups, func(group *proto.GroupDTO) bool {
		return strings.Contains(group.GetGroupName(), groupName)
	})

	if reqGroup != nil {
		framework.Logf("Successfully found Group %s", groupName)
	}

	return reqGroup
}

func validateNumbers(entityDTOs []*proto.EntityDTO, groupDTOs []*proto.GroupDTO, totalEntities, totalGroups int) {
	if len(entityDTOs) < totalEntities {
		framework.Failf("Entity count doesn't match post discovery: got: %d, expected (>=):  %d", len(entityDTOs), totalEntities)
	}
	if len(groupDTOs) < totalGroups {
		framework.Failf("Group count doesn't match post discovery: got: %d, expected (>=): %d", len(entityDTOs), totalGroups)
	}

	framework.Logf("Validated entities post discovery: got: %d, expected (>=):  %d", len(entityDTOs), totalEntities)
	framework.Logf("Validate groups post discovery: got: %d, expected (>=): %d", len(entityDTOs), totalGroups)
}

const float64EqualityThreshold = 1e-3

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) <= float64EqualityThreshold
}

func validateThresholds(entityDTOs []*proto.EntityDTO) {
	// the kind cluster is created with the below hard thresholds
	// "memory.available": "20%"
	// "nodefs.available": "20%",
	memoryUtilizationThresholdPct := float64(0)
	rootfsUtilizationThresholdPct := float64(0)
	imagefsUtilizationThresholdPct := float64(0)
	for _, entityDTO := range entityDTOs {
		if entityDTO.GetEntityType() == proto.EntityDTO_VIRTUAL_MACHINE {
			for _, soldCommodity := range entityDTO.GetCommoditiesSold() {
				if soldCommodity.GetCommodityType() == proto.CommodityDTO_VMEM {
					memoryUtilizationThresholdPct = soldCommodity.GetUtilizationThresholdPct()
				}
				if soldCommodity.GetCommodityType() == proto.CommodityDTO_VSTORAGE {
					if strings.HasSuffix(soldCommodity.GetKey(), "imagefs") {
						imagefsUtilizationThresholdPct = soldCommodity.GetUtilizationThresholdPct()
					} else {
						rootfsUtilizationThresholdPct = soldCommodity.GetUtilizationThresholdPct()
					}
				}
			}
			// Check any node
			break
		}
	}
	if !almostEqual(memoryUtilizationThresholdPct, float64(80)) ||
		!almostEqual(rootfsUtilizationThresholdPct, float64(80)) {
		framework.Failf("Thresholds not set correctly. found: MemoryUtilPct: %.9f, RootfsUtilPct: %.9f.\n"+
			"expected: MemoryUtilPct: %.9f, RootfsUtilPct:  %.9f.", memoryUtilizationThresholdPct,
			rootfsUtilizationThresholdPct, float64(80), float64(80))
	}

	// Because we ignore adding the imagefs commodity if the rootfs and imagefs partitions are same
	// we would not get a value for imagefs threshold from a kind cluster.
	// This can be uncommented on a cluster where imagefs is configured.
	if !almostEqual(imagefsUtilizationThresholdPct, float64(80)) {
		//		framework.Failf("Thresholds not set correctly. found: ImagefsUtilPct: %.9f.\n"+
		//			"expected:  ImagefsUtilPct:  %.9f.", imagefsUtilizationThresholdPct, float64(80))
	}

}

// There should not be any duplicates on either the sold commodities or bought
// commodities on an entity
func validateDuplicateCommodities(entityDTOs []*proto.EntityDTO) {
	for _, entityDTO := range entityDTOs {
		for i, commI := range entityDTO.GetCommoditiesSold() {
			for j, commJ := range entityDTO.GetCommoditiesSold() {
				if i == j {
					// same commodity in the slice
					continue
				}
				assertCommoditySame(commI, commJ, entityDTO)
			}
		}

		for _, commsBought := range entityDTO.GetCommoditiesBought() {
			for i, commI := range commsBought.GetBought() {
				for j, commJ := range commsBought.GetBought() {
					if i == j {
						// same commodity in the slice
						continue
					}
					assertCommoditySame(commI, commJ, entityDTO)
				}
			}
		}
	}
}

func assertCommoditySame(commI, commJ *proto.CommodityDTO, entityDTO *proto.EntityDTO) {
	if commI.GetCommodityType() == commJ.GetCommodityType() && commI.GetKey() == commJ.GetKey() {
		framework.Failf("Found duplicate commodity %s with key %s on Entity: %s/%s.",
			commI.GetCommodityType(), commI.GetKey(), entityDTO.GetEntityType(), entityDTO.GetDisplayName())
	}
}

func createProbeConfigOrDie(kubeClient *kubeclientset.Clientset, kubeletClient *kubeletclient.KubeletClient,
	dynamicClient dynamic.Interface, runtimeClient runtimeclient.Client) *configs.ProbeConfig {
	kubeletMonitoringConfig := kubelet.NewKubeletMonitorConfig(kubeletClient, kubeClient)
	clusterScraper := cluster.NewClusterScraper(nil, kubeClient, dynamicClient, runtimeClient, false, nil, "")
	masterMonitoringConfig := master.NewClusterMonitorConfig(clusterScraper)
	monitoringConfigs := []monitoring.MonitorWorkerConfig{
		kubeletMonitoringConfig,
		masterMonitoringConfig,
	}

	probeConfig := &configs.ProbeConfig{
		StitchingPropertyType: stitching.UUID,
		MonitoringConfigs:     monitoringConfigs,
		ClusterScraper:        clusterScraper,
		NodeClient:            kubeletClient,
	}

	return probeConfig
}

// This can also be driven by an arbitrary set of yamls in a test directory.
// TODO: In next step of this implementation, support will be added to
// run this against any cluster, where some utility test methods will
// discover all the kubernetes resources and then compare the numbers
// against the dtos discovered by kubeturbo routines.
func createResourcesForDiscovery(client *kubeclientset.Clientset, namespace string, replicas int32) ([]*appsv1.Deployment, error) {
	depResources := []*appsv1.Deployment{
		depMultiContainer(namespace, replicas, false),
		depSingleContainerWithResources(namespace, "", replicas, false, false, false, ""),
		deplMultiContainerWithResources(namespace, replicas),
	}

	var newDepResources []*appsv1.Deployment

	for _, dep := range depResources {
		// Can make this in parallel when the number of resources grows
		newDep, err := createDeployResource(client, dep)
		if err != nil {
			return nil, err
		}
		newDepResources = append(newDepResources, newDep)
	}

	return newDepResources, nil

}

func createDeployWithDuplicateAffinities(client *kubeclientset.Clientset, namespace string, replicas int32) error {
	// Can make this in parallel when the number of resources grows
	_, err := createDeployResource(client, depMultiContainer(namespace, 2, true))
	if err != nil {
		return err
	}
	return nil
}

// TODO: Improve this, either move this to yaml based resources or create lesser
// utility functions with more configurable properties and reduce code duplication.
func depMultiContainer(namespace string, replicas int32, withDuplicateAffinityRules bool) *appsv1.Deployment {
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-app-mc",
			Namespace:    namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test-app-mc",
				},
			},
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test-app-mc",
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
							Name:    "cont-test-app-mc-1",
							Image:   "busybox",
							Command: []string{"/bin/sh"},
							Args:    []string{"-c", "while true; do sleep 30; done;"},
						},
						{
							Name:    "cont-test-app-mc-2",
							Image:   "busybox",
							Command: []string{"/bin/sh"},
							Args:    []string{"-c", "while true; do sleep 30; done;"},
						},
					},
				},
			},
		},
	}

	if withDuplicateAffinityRules {
		dep.Spec.Template.Spec.Affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "kubernetes.io/arch",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"amd64"},
								},
							},
						},
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "kubernetes.io/arch",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"amd64"},
								},
							},
						},
					},
				},
			},
		}
	}
	return dep
}

func deplMultiContainerWithResources(namespace string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-",
			Namespace:    namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "test-app-mc-wr",
				},
			},
			Replicas: &replicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "test-app-mc-wr",
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
							Name:    "cont-test-app-mc-wr-1",
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
							Name:    "cont-test-app-mc-wr-2",
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
}
