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
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker/aggregation"
	kubeletclient "github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"github.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"github.com/turbonomic/kubeturbo/test/integration/framework"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
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
			kubeletClient := s.CreateKubeletClientOrDie(kubeConfig, kubeClient, "", "busybox", map[string]set.Set{}, true)

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
				app.DefaultDiscoverySamples, app.DefaultDiscoverySampleIntervalSec)

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
			_, err := createResourcesForDiscovery(kubeClient, namespace, 2, f.DockerImagePullSecretNames())
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

	Describe("Discovering affects of node with unknown state", func() {
		var entities []*proto.EntityDTO = nil
		var groups []*proto.GroupDTO = nil
		var deps *appsv1.Deployment = nil
		testName := "discovery-integration-test"
		nodeName := "kind-worker3"

		BeforeEach(func() {
			if f.Kubeconfig.CurrentContext != "kind-kind" {
				Skip("Skipping cluster that isn't using context kind-kind")
			}

			// create a pod to test and attach to the node soon to be in a NotReady state
			if deps == nil {
				var err error = nil
				deps, err = createDeployResource(kubeClient, depSingleContainerWithResources(namespace, "", 1, false, false, false, nodeName, nil))
				framework.ExpectNoError(err, "Error creating test resources")
			}
			// stop running node worker to simulate node in NotReady state
			execute("docker", "stop", nodeName)
			if err := wait.PollImmediate(framework.PollInterval, framework.DefaultSingleCallTimeout, func() (bool, error) {
				node, errInternal := kubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
				if errInternal != nil {
					glog.Errorf("Unexpected error while finding Node %s: %v", nodeName, errInternal)
					return false, nil
				}

				found := findOneNodeCondition(node.Status.Conditions, func(cond corev1.NodeCondition) bool {
					return cond.Type == "Ready" && cond.Status == "Unknown"
				})

				return found != nil, nil
			}); err != nil {
				framework.Failf("Failed to put node in a NotReady state. %v", err)
			}

			entityDTOs, groupDTOs, err := discoveryClient.DiscoverWithNewFramework(testName)
			if err != nil {
				framework.Failf(err.Error())
			}

			entities = entityDTOs
			groups = groupDTOs
		})

		AfterEach(func() {
			// restart node once finished with the test
			execute("docker", "start", nodeName)
			if err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
				node, errInternal := kubeClient.CoreV1().Nodes().Get(context.TODO(), nodeName, metav1.GetOptions{})
				if errInternal != nil {
					glog.Errorf("Unexpected error while finding Node %s: %v", nodeName, errInternal)
					return false, nil
				}
				found := findOneNodeCondition(node.Status.Conditions, func(cond corev1.NodeCondition) bool {
					return cond.Type == "Ready" && cond.Reason == "KubeletReady"
				})

				return found != nil, nil
			}); err != nil {
				framework.Failf("Failed to put node in a Ready state. %v", err)
			}

		})

		It("unknown node should be detected", func() {
			found := getUnknownNode(entities)
			if found == nil {
				framework.Failf("Node with unknown state not found")
			}

			if found.GetDisplayName() == "" {
				framework.Failf("unknown nodes should have a node name")
			}

		})

		It("unknown nodes should be listed in the unknown node group", func() {
			unknownNode := getUnknownNode(entities)
			unknownGroup := getUnknownNodeGroup(groups)

			if unknownGroup == nil {
				framework.Failf("Could not find Unknown Node group")
			}

			if unknownGroup.GetEntityType() != proto.EntityDTO_VIRTUAL_MACHINE {
				framework.Failf("Unknown node group as incorrect entity type")
			}

			if unknownGroup.GetDisplayName() != "All Nodes (State Unknown)" {
				framework.Failf("Unknown node group display name has been changed")
			}

			nodeInGroup := false
			for _, uuid := range unknownGroup.GetMemberList().GetMember() {
				if uuid == unknownNode.GetId() {
					nodeInGroup = true
				}
			}

			if !nodeInGroup {
				framework.Failf("Unknown node was not found in node group")
			}
		})

		It("should check that pods on unknown nodes also have status unknown", func() {
			node := getUnknownNode(entities)
			podsWithUnknownNode := findEntities(entities, func(entity *proto.EntityDTO) bool {
				return entity.GetEntityType() == proto.EntityDTO_CONTAINER_POD && findOneCommodityBought(entity.CommoditiesBought, func(commBought *proto.EntityDTO_CommodityBought) bool {
					return commBought.GetProviderId() == node.GetId()
				}) != nil
			}, len(entities))

			if len(podsWithUnknownNode) == 0 {
				framework.Failf("unknown node should have pods")
			}

			for _, pod := range podsWithUnknownNode {
				if pod.GetPowerState() != proto.EntityDTO_POWERSTATE_UNKNOWN {
					framework.Failf("All pods with unknown Node provider should be in an unknown state")
				}
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

func findNodeConditions(vals []corev1.NodeCondition, condition func(v corev1.NodeCondition) bool, howMany int) []corev1.NodeCondition {
	found := []corev1.NodeCondition{}
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

func findOneNodeCondition(values []corev1.NodeCondition, condition func(v corev1.NodeCondition) bool) *corev1.NodeCondition {
	found := findNodeConditions(values, condition, 1)
	if len(found) == 0 {
		return nil
	}
	return &found[0]
}

func execute(name string, arg ...string) {
	stdout, err := exec.Command(name, arg...).Output()
	if err != nil {
		glog.Error(err)
	}
	glog.Info("command output: " + string(stdout))
}

func getUnknownNode(entities []*proto.EntityDTO) *proto.EntityDTO {
	unknownNode := findOneEntity(entities, func(entity *proto.EntityDTO) bool {
		return entity.GetEntityType() == proto.EntityDTO_VIRTUAL_MACHINE && entity.GetPowerState() == proto.EntityDTO_POWERSTATE_UNKNOWN
	})
	if unknownNode != nil {
		framework.Logf("Successfully found Node with unknown power state.")
	}

	return unknownNode
}

func getUnknownNodeGroup(groups []*proto.GroupDTO) *proto.GroupDTO {
	unknownNodeGroup := findOneGroup(groups, func(group *proto.GroupDTO) bool {
		return strings.Contains(group.GetGroupName(), "All-Nodes-State-Unknown-")
	})

	if unknownNodeGroup != nil {
		framework.Logf("Successfully found Unknown Node Group")
	}

	return unknownNodeGroup
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
	clusterScraper := cluster.NewClusterScraper(kubeClient, dynamicClient, runtimeClient, false, nil, "")
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
func createResourcesForDiscovery(client *kubeclientset.Clientset, namespace string, replicas int32, pullSecrets []corev1.LocalObjectReference) ([]*appsv1.Deployment, error) {
	depResources := []*appsv1.Deployment{
		depMultiContainer(namespace, replicas, false),
		depSingleContainerWithResources(namespace, "", replicas, false, false, false, "", pullSecrets),
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
