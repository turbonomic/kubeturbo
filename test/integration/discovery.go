package integration

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/test/integration/framework"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	kubeclientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	. "github.com/onsi/ginkgo"
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
			kubeletClient := s.CreateKubeletClientOrDie(kubeConfig, kubeClient, "busybox", true)

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

			// Kubernetes Probe Discovery Client
			discoveryClient = discovery.NewK8sDiscoveryClient(discoveryClientConfig)
		}
		namespace = f.TestNamespaceName()
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

			entityDTOs, groupDTOs, err := discoveryClient.DiscoverWithNewFramework("discovery-integration-test")
			framework.ExpectNoError(err, "Failed completing discovery of test cluster")

			// We run this against a kind cluster with 1 master and 2 workers created
			// using utility script ./hack/create_kind_cluster.sh
			// The preexisting entities, i.e. control plane components result in a
			// total of 57 entity DTOs and 23 group DTOs.
			// New resources included add up to a total of 91 entities and 33 group DTOs.
			validateDTOs(entityDTOs, groupDTOs, 91, 33)
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

func validateDTOs(entityDTOs []*proto.EntityDTO, groupDTOs []*proto.GroupDTO, totalEntities, totalGroups int) {
	if len(entityDTOs) != totalEntities {
		framework.Failf("Entity count doesn't match post discovery: got: %d, expected: %d", len(entityDTOs), totalEntities)
	}
	if len(groupDTOs) != totalGroups {
		framework.Failf("Group count doesn't match post discovery: got: %d, expected: %d", len(entityDTOs), totalGroups)
	}
}

func createProbeConfigOrDie(kubeClient *kubeclientset.Clientset, kubeletClient *kubeletclient.KubeletClient, dynamicClient dynamic.Interface) *configs.ProbeConfig {
	kubeletMonitoringConfig := kubelet.NewKubeletMonitorConfig(kubeletClient, kubeClient)
	clusterScraper := cluster.NewClusterScraper(kubeClient, dynamicClient)
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
		depMultiContainer(namespace, replicas),
		depSingleContainerWithResources(namespace, replicas),
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

// TODO: Improve this, either move this to yaml based resources or create lesser
// utility functions with more configurable properties and reduce code duplication.
func depMultiContainer(namespace string, replicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
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
