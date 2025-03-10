package integration

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"os/exec"

	set "github.com/deckarep/golang-set"
	"github.com/golang/glog"
	. "github.com/onsi/ginkgo"
	"k8s.io/client-go/dynamic"
	kubeclientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.ibm.com/turbonomic/kubeturbo/cmd/kubeturbo/app"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/aggregation"
	"github.ibm.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"github.ibm.com/turbonomic/kubeturbo/test/integration/framework"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
)

const (
	STUBS_ENTITY_COUNT = 513
)

var isWiremockServerRunning = false
var wiremockImage = "icr.io/cpopen/turbonomic/wiremock:3.3.1"

/*
Pre-requisite: to run these tests you need the following setup:
1. Launch wiremock server locally where the host URL corresponds to WIREMOCK_HOST and mount the mapping files specific to these test when starting the server.
2. Create a fake kubeconfig file where the server URL points to the wiremock server. See wiremock/wiremock.config as an example.
Note:
  - The tests are grouped by the dataset they use. All the tests within the same describe block must use the same dataset.
  - All the tests within the describe block can only run sequentially since multuple tests targetting the same server can lead to unexpected results
    if they call the same API (i.e. the scenario state might change). Currently, all the describe blocks in this file must also run sequentially since
	we only deploy one docker container at a time. In the future, these can possibly can parallelized where we deploy one container per describe block
	and each one uses a different port.
*/

var _ = Describe("Wiremock Tests", func() {
	f := framework.NewTestFramework("wiremock-test")
	var kubeConfig *restclient.Config
	var kubeClient *kubeclientset.Clientset
	var discoveryClient *discovery.K8sDiscoveryClient
	var wiremockHost string
	var wiremockDataPath string
	// Absolute path of kubeturbo-wiremock repo
	wiremockDataRootDir := getWiremockDataRootDir()

	Describe("aedev-rosa-discovery", func() {

		BeforeEach(func() {
			f.BeforeEach()
			if !isWiremockServerRunning {
				wiremockDataPath = wiremockDataRootDir + "/aedev-rosa-discovery"
				deployWiremockContainer(wiremockDataPath)
			}

			if kubeConfig == nil {
				kubeConfig = f.GetKubeConfig()
				kubeClient = f.GetKubeClient("wiremock-test")
				dynamicClient, err := dynamic.NewForConfig(kubeConfig)
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
				targetConfig := configs.K8sTargetConfig{}
				targetConfig.ServerVersionOcp = "0.0"
				discoveryClientConfig := discovery.NewDiscoveryConfig(probeConfig, &targetConfig, app.DefaultValidationWorkers,
					app.DefaultValidationTimeout, aggregation.DefaultContainerUtilizationDataAggStrategy,
					aggregation.DefaultContainerUsageDataAggStrategy, ormClient, app.DefaultDiscoveryWorkers, app.DefaultDiscoveryTimeoutSec,
					app.DefaultDiscoverySamples, app.DefaultDiscoverySampleIntervalSec, 0)

				// Kubernetes Probe Discovery Client
				discoveryClient = discovery.NewK8sDiscoveryClient(discoveryClientConfig)
				wiremockHost = kubeConfig.Host

				// Disable GoMemLimit so list API returns all the items instead of an arbitrary limit
				features := map[string]bool{"GoMemLimit": false}
				err = utilfeature.DefaultMutableFeatureGate.SetFromMap(features)
				framework.ExpectNoError(err, "Could not set Feature Gates")
			}

			// reset scenario states between each test
			resetWiremockScenarios(kubeConfig.Host)

		})

		It("Sample wiremock test using recorded data", func() {
			resp, err := http.Get(wiremockHost + "/api/v1/namespaces")
			framework.ExpectNoError(err, "Error making http request")
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				_, err := io.ReadAll(resp.Body)
				if err != nil {
					framework.Failf("Error reading response body: %v", err)
				}
			} else {
				framework.Failf("Status code is %v, but should be 200.", resp.StatusCode)
			}
		})

		It("Sample discovery", func() {
			entityDTOs, _, err := discoveryClient.DiscoverWithNewFramework("wiremock-discovery-integration-test")
			framework.ExpectNoError(err, "Failed completing discovery of test cluster")
			if len(entityDTOs) != STUBS_ENTITY_COUNT {
				framework.Failf("Expected to find %d entityDTOs, but found %d!", STUBS_ENTITY_COUNT, len(entityDTOs))
			}

			entityCounts := make(map[proto.EntityDTO_EntityType]int64)
			for _, entityDTO := range entityDTOs {
				_, found := entityCounts[*entityDTO.EntityType]
				if found {
					entityCounts[*entityDTO.EntityType] += 1
				} else {
					entityCounts[*entityDTO.EntityType] = 1
				}
			}
			for key, val := range entityCounts {
				glog.Info(key, val)
			}
		})

		It("Stop wiremock server", func() {
			stopWiremockContainer()
		})
	})
})

func deployWiremockContainer(dataPath string) {
	mountDirectory := "/home/wiremock"
	cmd := exec.Command("docker", "run", "--rm", "-d", "-it", "-p", "8080:8080", "--name", "wiremock", "-v", dataPath+":"+mountDirectory, wiremockImage, "--root-dir", mountDirectory)
	err := cmd.Run()
	if err != nil {
		glog.Fatalf("Failed to deploy wiremock server: %v.", err)
	}
	glog.Infof("Wiremock server started with data loaded from %v!", dataPath)
	isWiremockServerRunning = true
	time.Sleep(time.Second * 10)
	// debug only: check if mappings are loaded correctly
	// should see all the mappings.json file in the output
	// out, _ := exec.Command("curl", "http://localhost:8080/__admin/mappings").Output()
	// glog.Info(string(out))
}

func stopWiremockContainer() {
	cmd := exec.Command("docker", "stop", "wiremock")
	err := cmd.Run()
	if err != nil {
		glog.Fatalf("Failed to stop wiremock server: %v.", err)
	}
	glog.Info("Stopped wiremock server!")
	isWiremockServerRunning = false
	time.Sleep(time.Second * 5)
}

func resetWiremockScenarios(host string) {
	url := host + "/__admin/scenarios/reset"

	r, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(`{}`)))
	if err != nil {
		glog.Error(err)
	}

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		glog.Error(err)
	}
	if resp.StatusCode != http.StatusOK {
		glog.Error("Scenario states not reset between tests!")
	}
}

func getWiremockDataRootDir() string {
	rootDir := framework.TestContext.WiremockDataRootDir
	// By default it assumes the pattern in travis where kubeturbo-wiremock is cloned
	// to same directory as kubeturbo and tests are run through ./build/integration.test
	if rootDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			panic(err)
		}
		rootDir = filepath.Join(filepath.Dir(wd), "wiremock-data")
	}
	return rootDir
}
