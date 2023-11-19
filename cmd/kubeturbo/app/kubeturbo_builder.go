package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	set "github.com/deckarep/golang-set"
	"github.com/fsnotify/fsnotify"
	"github.com/golang/glog"
	osclient "github.com/openshift/client-go/apps/clientset/versioned"
	clusterclient "github.com/openshift/machine-api-operator/pkg/generated/clientset/versioned"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	apiv1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	versionhelper "k8s.io/apimachinery/pkg/version"
	"k8s.io/apiserver/pkg/server/healthz"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"

	kubeturbo "github.ibm.com/turbonomic/kubeturbo/pkg"
	"github.ibm.com/turbonomic/kubeturbo/pkg/action/executor/gitops"
	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/processor"
	nodeUtil "github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker"
	agg "github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/aggregation"
	"github.ibm.com/turbonomic/kubeturbo/pkg/features"
	"github.ibm.com/turbonomic/kubeturbo/pkg/kubeclient"
	"github.ibm.com/turbonomic/kubeturbo/pkg/resourcemapping"
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
	"github.ibm.com/turbonomic/kubeturbo/test/flag"
	gitopsv1alpha1 "github.ibm.com/turbonomic/turbo-gitops/api/v1alpha1"
	policyv1alpha1 "github.ibm.com/turbonomic/turbo-policy/api/v1alpha1"
)

const (
	// The default port for vmt service server
	KubeturboPort                     = 10265
	DefaultKubeletPort                = 10255
	DefaultKubeletHttps               = false
	defaultVMPriority                 = -1
	defaultVMIsBase                   = true
	defaultDiscoveryIntervalSec       = 600
	DefaultValidationWorkers          = 10
	DefaultValidationTimeout          = 60
	DefaultDiscoveryWorkers           = 10
	DefaultDiscoveryTimeoutSec        = 180
	DefaultDiscoverySamples           = 10
	DefaultDiscoverySampleIntervalSec = 60
	DefaultGCIntervalMin              = 10
	DefaultReadinessRetryThreshold    = 60
)

var (
	defaultSccSupport = []string{"*"}

	// these variables will be deprecated. Keep it here for backward compatibility only
	k8sVersion        = "1.8"
	noneSchedulerName = "turbo-no-scheduler"

	// custom resource scheme for controller runtime client
	customScheme = runtime.NewScheme()
)

type cleanUp func()

func init() {
	// Add registered custom types to the custom scheme
	utilruntime.Must(policyv1alpha1.AddToScheme(customScheme))
	utilruntime.Must(gitopsv1alpha1.AddToScheme(customScheme))
}

// VMTServer has all the context and params needed to run a Scheduler
// TODO: leaderElection is disabled now because of dependency problems.
type VMTServer struct {
	Port                 int
	Address              string
	Master               string
	K8sTAPSpec           string
	TestingFlagPath      string
	KubeConfig           string
	BindPodsQPS          float32
	BindPodsBurst        int
	DiscoveryIntervalSec int

	// LeaderElection componentconfig.LeaderElectionConfiguration

	EnableProfiling bool

	// To stitch the Nodes in Kubernetes cluster with the VM from the underlying cloud or
	// hypervisor infrastructure: either use VM UUID or VM IP.
	// If the underlying infrastructure is VMWare, AWS instances, or Azure instances, VM's UUID is used.
	UseUUID bool

	// VMPriority: priority of VM in supplyChain definition from kubeturbo, should be less than 0;
	VMPriority int32
	// VMIsBase: Is VM is the base template from kubeturbo, when stitching with other VM probes, should be false;
	VMIsBase bool

	// Kubelet related config
	KubeletPort          int
	EnableKubeletHttps   bool
	UseNodeProxyEndpoint bool

	// The cluster processor related config
	ValidationWorkers int
	ValidationTimeout int

	// Discovery related config
	DiscoveryWorkers    int
	DiscoveryTimeoutSec int

	// Data sampling discovery related config
	DiscoverySamples           int
	DiscoverySampleIntervalSec int

	// Garbage collection (leaked pods) interval config
	GCIntervalMin int

	// Number of workload controller items the list api call should request for
	ItemsPerListQuery int

	// The Openshift SCC list allowed for action execution
	sccSupport []string

	// Injected Cluster Key to enable pod move across cluster
	ClusterKeyInjected string

	// Force the use of self-signed certificates.
	// The default is true.
	ForceSelfSignedCerts bool

	// Don't try to move pods which have volumes attached
	// If set to false kubeturbo can still try to move such pods.
	FailVolumePodMoves bool

	// Try to update namespace quotas to allow pod moves when one or
	// more quota(s) is/are full.
	UpdateQuotaToAllowMoves bool

	// The Cluster API namespace
	ClusterAPINamespace string

	// Busybox image uri used for cpufreq getter job
	BusyboxImage string

	// Name of the secret that stores the image pull credentials of cpu freq getter job image
	BusyboxImagePullSecret string

	// CpufreqJobExcludeNodeLabels is used to specify node labels for nodes to be
	// excluded from running cpufreq job
	CpufreqJobExcludeNodeLabels string

	// Strategy to aggregate Container utilization data on ContainerSpec entity
	containerUtilizationDataAggStrategy string
	// Strategy to aggregate Container usage data on ContainerSpec entity
	containerUsageDataAggStrategy string
	// Total number of retrys. When a pod is not ready, Kubeturbo will try failureThreshold times before giving up
	readinessRetryThreshold int
	// Git configuration for gitops based action execution
	gitConfig gitops.GitConfig

	// Cpu frequency getter, used to replace busybox
	CpuFrequencyGetterImage string
	// Name of the secret that stores the image pull credentials of cpu freq getter job image
	CpuFrequencyGetterPullSecret string
	// Cleanup resources created in the SCC impersonation
	CleanupSccRelatedResources bool
}

// NewVMTServer creates a new VMTServer with default parameters
func NewVMTServer() *VMTServer {
	s := VMTServer{
		Port:       KubeturboPort,
		Address:    "127.0.0.1",
		VMPriority: defaultVMPriority,
		VMIsBase:   defaultVMIsBase,
	}
	return &s
}

// AddFlags adds flags for a specific VMTServer to the specified FlagSet
func (s *VMTServer) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.ClusterKeyInjected, "cluster-key-injected", "", "Injected cluster key to enable pod move across cluster")
	fs.IntVar(&s.Port, "port", s.Port, "The port that kubeturbo's http service runs on.")
	fs.StringVar(&s.Address, "ip", s.Address, "the ip address that kubeturbo's http service runs on.")
	// TODO: The flagset that is included by vendoring k8s uses the same names i.e. "master" and "kubeconfig".
	// This for some reason conflicts with the names introduced by kubeturbo after upgrading the k8s vendored code
	// to version 1.19.1. Right now we have changed the names of kubeturbo flags as a quick fix. These flags are
	// not user facing and are useful only when running kubeturbo outside the cluster. Find a better solution
	// when need be.
	fs.StringVar(&s.Master, "k8s-master", s.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig).")
	fs.StringVar(&s.K8sTAPSpec, "turboconfig", s.K8sTAPSpec, "Path to the config file.")
	fs.StringVar(&s.TestingFlagPath, "testingflag", s.TestingFlagPath, "Path to the testing flag.")
	fs.StringVar(&s.KubeConfig, "k8s-kubeconfig", s.KubeConfig, "Path to kubeconfig file with authorization and master location information.")
	fs.BoolVar(&s.EnableProfiling, "profiling", false, "Enable profiling via web interface host:port/debug/pprof/.")
	fs.BoolVar(&s.UseUUID, "stitch-uuid", true, "Use VirtualMachine's UUID to do stitching, otherwise IP is used.")
	fs.IntVar(&s.KubeletPort, "kubelet-port", DefaultKubeletPort, "The port of the kubelet runs on.")
	fs.BoolVar(&s.EnableKubeletHttps, "kubelet-https", DefaultKubeletHttps, "Indicate if Kubelet is running on https server.")
	fs.BoolVar(&s.UseNodeProxyEndpoint, "use-node-proxy-endpoint", false, "Indicate if Kubelet queries should be routed through APIServer node proxy endpoint.")
	fs.BoolVar(&s.ForceSelfSignedCerts, "kubelet-force-selfsigned-cert", true, "Indicate if we must use self-signed cert.")
	fs.BoolVar(&s.FailVolumePodMoves, "fail-volume-pod-moves", true, "Indicate if kubeturbo should fail to move pods which have volumes attached. Default is set to true.")
	fs.BoolVar(&s.UpdateQuotaToAllowMoves, "update-quota-to-allow-moves", true, "Indicate if kubeturbo should try to update namespace quotas to allow pod moves when quota(s) is/are full. Default is set to true.")
	fs.StringVar(&k8sVersion, "k8sVersion", k8sVersion, "[deprecated] the kubernetes server version; for openshift, it is the underlying Kubernetes' version.")
	fs.StringVar(&noneSchedulerName, "noneSchedulerName", noneSchedulerName, "[deprecated] a none-exist scheduler name, to prevent controller to create Running pods during move Action.")
	fs.IntVar(&s.DiscoveryIntervalSec, "discovery-interval-sec", defaultDiscoveryIntervalSec, "The discovery interval in seconds.")
	fs.IntVar(&s.ValidationWorkers, "validation-workers", DefaultValidationWorkers, "The validation workers")
	fs.IntVar(&s.ValidationTimeout, "validation-timeout-sec", DefaultValidationTimeout, "The validation timeout in seconds.")
	fs.IntVar(&s.DiscoveryWorkers, "discovery-workers", DefaultDiscoveryWorkers, "The number of discovery workers.")
	fs.IntVar(&s.DiscoveryTimeoutSec, "discovery-timeout-sec", DefaultDiscoveryTimeoutSec, "The discovery timeout in seconds for each discovery worker.")
	fs.IntVar(&s.DiscoverySamples, "discovery-samples", DefaultDiscoverySamples, "The number of resource usage data samples to be collected from kubelet in each full discovery cycle. This should be no larger than 60.")
	fs.IntVar(&s.DiscoverySampleIntervalSec, "discovery-sample-interval", DefaultDiscoverySampleIntervalSec, "The discovery interval in seconds to collect additional resource usage data samples from kubelet. This should be no smaller than 10 seconds.")
	fs.IntVar(&s.GCIntervalMin, "garbage-collection-interval", DefaultGCIntervalMin, "The garbage collection interval in minutes for possible leaked pods from actions failed because of kubeturbo restarts. Default value is 20 mins.")
	fs.IntVar(&s.ItemsPerListQuery, "items-per-list-query", 0, "Number of workload controller items the list api call should request for.")
	fs.StringSliceVar(&s.sccSupport, "scc-support", defaultSccSupport, "The SCC list allowed for executing pod actions, e.g., --scc-support=restricted,anyuid or --scc-support=* to allow all. Default allowed scc is [*].")
	// So far we have noticed cluster api support only in openshift clusters and our implementation works only for openshift
	// It thus makes sense to have openshifts machine api namespace as our default cluster api namespace
	fs.StringVar(&s.ClusterAPINamespace, "cluster-api-namespace", "openshift-machine-api", "The Cluster API namespace.")
	fs.StringVar(&s.BusyboxImage, "busybox-image", "busybox", "The complete image uri used for fallback node cpu frequency getter job.")
	fs.StringVar(&s.BusyboxImagePullSecret, "busybox-image-pull-secret", "", "The name of the secret that stores the image pull credentials for busybox image.")
	fs.StringVar(&s.CpufreqJobExcludeNodeLabels, "cpufreq-job-exclude-node-labels", "", "The comma separated list of key=value node label pairs for the nodes (for example windows nodes) to be excluded from running job based cpufrequency getter.")
	fs.StringVar(&s.containerUtilizationDataAggStrategy, "cnt-utilization-data-agg-strategy", agg.DefaultContainerUtilizationDataAggStrategy, "Container utilization data aggregation strategy.")
	fs.StringVar(&s.containerUsageDataAggStrategy, "cnt-usage-data-agg-strategy", agg.DefaultContainerUsageDataAggStrategy, "Container usage data aggregation strategy.")
	fs.IntVar(&s.readinessRetryThreshold, "readiness-retry-threshold", DefaultReadinessRetryThreshold, "When the pod readiness check fails, Kubeturbo will try readinessRetryThreshold times before giving up. Defaults to 60.")
	// Flags for gitops based action execution
	fs.StringVar(&s.gitConfig.GitSecretNamespace, "git-secret-namespace", "", "The namespace of the secret which holds the git credentials.")
	fs.StringVar(&s.gitConfig.GitSecretName, "git-secret-name", "", "The name of the secret which holds the git credentials.")
	fs.StringVar(&s.gitConfig.GitUsername, "git-username", "", "The user name to be used to push changes to git.")
	fs.StringVar(&s.gitConfig.GitEmail, "git-email", "", "The email to be used to push changes to git.")
	fs.StringVar(&s.gitConfig.CommitMode, "git-commit-mode", "direct", "The commit mode that should be used for git action executions. One of request|direct. Defaults to direct.")
	// CpuFreqGetter image and secret
	fs.StringVar(&s.CpuFrequencyGetterImage, "cpufreqgetter-image", "icr.io/cpopen/turbonomic/cpufreqgetter", "The complete cpufreqgetter image uri used for fallback node cpu frequency getter job.")
	fs.StringVar(&s.CpuFrequencyGetterPullSecret, "cpufreqgetter-image-pull-secret", "", "The name of the secret that stores the image pull credentials for cpufreqgetter image.")
	fs.BoolVar(&s.CleanupSccRelatedResources, "cleanup-scc-impersonation-resources", true, "Enable cleanup the resources for scc impersonation.")
	fs.String("satellite-location-provider", "", "The IBM cloud satellite location provider, it only supports azure as of today. Defaults to none.")
}

// create an eventRecorder to send events to Kubernetes APIserver
func createRecorder(kubecli *kubernetes.Clientset) record.EventRecorder {
	// Create a new broadcaster which will send events we generate to the apiserver
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: v1core.New(kubecli.CoreV1().RESTClient()).Events(apiv1.NamespaceAll),
	})
	// this EventRecorder can be used to send events to this EventBroadcaster
	// with the given event source.
	return eventBroadcaster.NewRecorder(scheme.Scheme, apiv1.EventSource{Component: "kubeturbo"})
}

func (s *VMTServer) createKubeConfigOrDie() *restclient.Config {
	kubeConfig, err := clientcmd.BuildConfigFromFlags(s.Master, s.KubeConfig)
	if err != nil {
		glog.Errorf("Fatal error: failed to get kubeconfig:  %s", err)
		os.Exit(1)
	}
	// This specifies the number and the max number of query per second to the api server.
	kubeConfig.QPS = 20.0
	kubeConfig.Burst = 30

	return kubeConfig
}

func (s *VMTServer) createKubeClientOrDie(kubeConfig *restclient.Config) *kubernetes.Clientset {
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		glog.Errorf("Fatal error: failed to create kubeClient:%v", err)
		os.Exit(1)
	}

	return kubeClient
}

func (s *VMTServer) ensureBusyboxImageBackwardCompatibility() {
	if s.CpuFrequencyGetterImage == "icr.io/cpopen/turbonomic/cpufreqgetter" && s.BusyboxImage != "busybox" {
		// Somebody has set --busybox-image only explicitly, for example coming from an old configuration
		// we should use it
		s.CpuFrequencyGetterImage = s.BusyboxImage
	}
	// Other cases for example below, --cpufreqgetter-image value will always take precedence:
	// if s.CpuFrequencyGetterImage == "icr.io/cpopen/turbonomic/cpufreqgetter" && s.BusyboxImage == "busybox"
	// if s.CpuFrequencyGetterImage != "icr.io/cpopen/turbonomic/cpufreqgetter" && s.BusyboxImage != "busybox"
	// if s.CpuFrequencyGetterImage == "icr.io/cpopen/turbonomic/cpufreqgetter" && s.BusyboxImage != "busybox"

	if s.CpuFrequencyGetterPullSecret == "" && s.BusyboxImagePullSecret != "" {
		// Somebody has set --busybox-image-pull-secret	only explicitly, for example coming from an old configuration
		// we should use it
		s.CpuFrequencyGetterPullSecret = s.BusyboxImagePullSecret
	}
	// Other cases for example below, --cpufreqgetter-image-pull-secret value will always take precedence:
	// if s.CpuFrequencyGetterPullSecret != "" && s.BusyboxImagePullSecret != ""
	// if s.CpuFrequencyGetterPullSecret != "" && s.BusyboxImagePullSecret == ""
}

func (s *VMTServer) CreateKubeletClientOrDie(kubeConfig *restclient.Config, fallbackClient *kubernetes.Clientset,
	cpuFreqGetterImage, imagePullSecret string, cpufreqJobExcludeNodeLabels map[string]set.Set, useProxyEndpoint bool,
) *kubeclient.KubeletClient {
	kubeletClient, err := kubeclient.NewKubeletConfig(kubeConfig).
		WithPort(s.KubeletPort).
		EnableHttps(s.EnableKubeletHttps).
		ForceSelfSignedCerts(s.ForceSelfSignedCerts).
		// Timeout(to).
		Create(fallbackClient, cpuFreqGetterImage, imagePullSecret, cpufreqJobExcludeNodeLabels, useProxyEndpoint)
	if err != nil {
		glog.Errorf("Fatal error: failed to create kubeletClient: %v", err)
		os.Exit(1)
	}

	return kubeletClient
}

func (s *VMTServer) checkFlag() error {
	if s.KubeConfig == "" && s.Master == "" {
		glog.Warningf("Neither --kubeconfig nor --master was specified.  Using default API client.  This might not work.")
	}

	if s.Master != "" {
		glog.V(3).Infof("Master is %s", s.Master)
	}

	if s.TestingFlagPath != "" {
		flag.SetPath(s.TestingFlagPath)
	}

	ip := net.ParseIP(s.Address)
	if ip == nil {
		return fmt.Errorf("wrong ip format:%s", s.Address)
	}

	if s.Port < 1 {
		return fmt.Errorf("Port[%d] should be bigger than 0.", s.Port)
	}

	if s.KubeletPort < 1 {
		return fmt.Errorf("[KubeletPort[%d] should be bigger than 0.", s.KubeletPort)
	}

	return nil
}

// Run runs the specified VMTServer.  This should never exit.
func (s *VMTServer) Run() {
	if err := s.checkFlag(); err != nil {
		glog.Fatalf("Check flag failed: %v. Abort.", err.Error())
	}

	kubeConfig := s.createKubeConfigOrDie()
	glog.V(3).Infof("kubeConfig: %+v", kubeConfig)

	kubeClient := s.createKubeClientOrDie(kubeConfig)

	// Create controller runtime client that support custom resources
	runtimeClient, err := runtimeclient.New(kubeConfig, runtimeclient.Options{Scheme: customScheme})
	if err != nil {
		glog.Fatalf("Failed to create controller runtime client: %v.", err)
	}

	// Openshift client for deploymentconfig resize forced rollouts
	osClient, err := osclient.NewForConfig(kubeConfig)
	if err != nil {
		glog.Fatalf("Failed to generate openshift client for kubernetes target: %v", err)
	}

	// TODO: Replace dynamicClient with runtimeClient
	dynamicClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		glog.Fatalf("Failed to generate dynamic client for kubernetes target: %v", err)
	}

	util.K8sAPIDeploymentGV, err = discoverk8sAPIResourceGV(kubeClient, util.DeploymentResName)
	if err != nil {
		glog.Warningf("Failure in discovering k8s deployment API group/version: %v", err.Error())
	}
	glog.V(2).Infof("Using group version %v for k8s deployments", util.K8sAPIDeploymentGV)

	util.K8sAPIReplicasetGV, err = discoverk8sAPIResourceGV(kubeClient, util.ReplicaSetResName)
	if err != nil {
		glog.Warningf("Failure in discovering k8s replicaset API group/version: %v", err.Error())
	}
	glog.V(2).Infof("Using group version %v for k8s replicasets", util.K8sAPIReplicasetGV)

	glog.V(3).Infof("Turbonomic config path is: %v", s.K8sTAPSpec)

	k8sTAPSpec, err := kubeturbo.ParseK8sTAPServiceSpec(s.K8sTAPSpec, kubeConfig.Host)
	if err != nil {
		glog.Fatalf("Failed to generate correct TAP config: %v", err.Error())
	}

	if k8sTAPSpec.FeatureGates != nil {
		err = utilfeature.DefaultMutableFeatureGate.SetFromMap(k8sTAPSpec.FeatureGates)
		if err != nil {
			glog.Fatalf("Invalid Feature Gates: %v", err)
		}
	}

	if utilfeature.DefaultFeatureGate.Enabled(features.GoMemLimit) {
		glog.V(2).Info("Memory Optimisations are enabled.")
		// AUTOMEMLIMIT_DEBUG environment variable enables debug logging of AUTOMEMLIMIT
		// GoMemLimit will be set during the start of each discovery, see K8sDiscoveryClient.Discover,
		// as memory limit may change overtime
		_ = os.Setenv("AUTOMEMLIMIT_DEBUG", "true")
		if s.ItemsPerListQuery != 0 {
			// Perform sanity check on user specified value of itemsPerListQuery
			if s.ItemsPerListQuery < processor.DefaultItemsPerGiMemory {
				var errMsg string
				if s.ItemsPerListQuery < 0 {
					errMsg = "negative"
				} else {
					errMsg = "set too low"
				}
				glog.Warningf("Argument --items-per-list-query is %s (%v). Setting it to the default value of %d.",
					errMsg, s.ItemsPerListQuery, processor.DefaultItemsPerGiMemory)
				s.ItemsPerListQuery = processor.DefaultItemsPerGiMemory
			} else {
				glog.V(2).Infof("Set items per list API call to the user specified value: %v.", s.ItemsPerListQuery)
			}
		}
	} else {
		glog.V(2).Info("Memory Optimisations are not enabled.")
	}

	// Collect target and probe info such as master host, server version, probe container image, etc
	k8sTAPSpec.CollectK8sTargetAndProbeInfo(kubeConfig, kubeClient)

	excludeLabelsMap, err := nodeUtil.LabelMapFromNodeSelectorString(s.CpufreqJobExcludeNodeLabels)
	if err != nil {
		glog.Fatalf("Invalid cpu frequency exclude node label selectors: %v. The selectors "+
			"should be a comma saperated list of key=value node label pairs", err)
	}

	s.ensureBusyboxImageBackwardCompatibility()
	kubeletClient := s.CreateKubeletClientOrDie(kubeConfig, kubeClient, s.CpuFrequencyGetterImage,
		s.CpuFrequencyGetterPullSecret, excludeLabelsMap, s.UseNodeProxyEndpoint)
	caClient, err := clusterclient.NewForConfig(kubeConfig)
	if err != nil {
		glog.Errorf("Failed to generate correct TAP config: %v", err.Error())
		caClient = nil
	}

	// Interface to discover turbonomic ORM mappings (legacy and v2) for resize actions
	ormClientManager := resourcemapping.NewORMClientManager(dynamicClient, kubeConfig)

	// Configuration for creating the Kubeturbo TAP service
	vmtConfig := kubeturbo.NewVMTConfig2()
	vmtConfig.WithTapSpec(k8sTAPSpec).
		WithKubeClient(kubeClient).
		WithKubeConfig(kubeConfig).
		WithDynamicClient(dynamicClient).
		WithControllerRuntimeClient(runtimeClient).
		WithORMClientManager(ormClientManager).
		WithKubeletClient(kubeletClient).
		WithClusterAPIClient(caClient).
		WithOpenshiftClient(osClient).
		WithVMPriority(s.VMPriority).
		WithVMIsBase(s.VMIsBase).
		UsingUUIDStitch(s.UseUUID).
		WithDiscoveryInterval(s.DiscoveryIntervalSec).
		WithValidationTimeout(s.ValidationTimeout).
		WithValidationWorkers(s.ValidationWorkers).
		WithDiscoveryWorkers(s.DiscoveryWorkers).
		WithDiscoveryTimeout(s.DiscoveryTimeoutSec).
		WithDiscoverySamples(s.DiscoverySamples).
		WithDiscoverySampleIntervalSec(s.DiscoverySampleIntervalSec).
		WithSccSupport(s.sccSupport).
		WithCAPINamespace(s.ClusterAPINamespace).
		WithContainerUtilizationDataAggStrategy(s.containerUtilizationDataAggStrategy).
		WithContainerUsageDataAggStrategy(s.containerUsageDataAggStrategy).
		WithVolumePodMoveConfig(s.FailVolumePodMoves).
		WithQuotaUpdateConfig(s.UpdateQuotaToAllowMoves).
		WithReadinessRetryThreshold(s.readinessRetryThreshold).
		WithClusterKeyInjected(s.ClusterKeyInjected).
		WithItemsPerListQuery(s.ItemsPerListQuery)

	if utilfeature.DefaultFeatureGate.Enabled(features.GitopsApps) {
		vmtConfig.WithGitConfig(s.gitConfig)
	} else {
		if s.gitConfig.GitEmail != "" ||
			s.gitConfig.GitSecretName != "" ||
			s.gitConfig.GitSecretNamespace != "" ||
			s.gitConfig.GitUsername != "" {
			glog.V(2).Infof("Feature: %v is not enabled, arg values set for git-email: %s, git-username: %s "+
				"git-secret-name: %s, git-secret-namespace: %s will be ignored.", features.GitopsApps,
				s.gitConfig.GitEmail, s.gitConfig.GitUsername, s.gitConfig.GitSecretName, s.gitConfig.GitSecretNamespace)
		}
	}
	glog.V(3).Infof("Finished creating turbo configuration: %+v", vmtConfig)

	// The KubeTurbo TAP service
	k8sTAPService, err := kubeturbo.NewKubernetesTAPService(vmtConfig)
	if err != nil {
		glog.Fatalf("Unexpected error while creating Kubernetes TAP service: %s", err)
	}

	// Its a must to include the namespace env var in the kubeturbo pod spec.
	ns := util.GetKubeturboNamespace()

	if k8sTAPSpec.K8sTargetConfig.IsOpenShift() {
		// Update scc resources in parallel.
		go ManageSCCs(ns, dynamicClient, kubeClient)
	} else {
		glog.V(2).Infof("Skip processing SCC for non-OpenShift cluster")
	}

	// The client for healthz, debug, and prometheus
	go s.startHttp()

	cleanupWG := &sync.WaitGroup{}
	cleanupSCCFn := func() {
		ns := util.GetKubeturboNamespace()
		CleanUpSCCMgmtResources(ns, dynamicClient, kubeClient)
	}
	disconnectFn := func() {
		// Disconnect from Turbo server when Kubeturbo is shutdown
		// Close the mediation container including the endpoints. It avoids the
		// invalid endpoints remaining in the server side. See OM-28801.
		k8sTAPService.DisconnectFromTurbo()
	}
	var cleanupFuns []cleanUp
	if s.CleanupSccRelatedResources {
		cleanupFuns = append(cleanupFuns, cleanupSCCFn)
	}
	cleanupFuns = append(cleanupFuns, disconnectFn)
	handleExit(cleanupWG, cleanupFuns...)

	gCChan := make(chan bool)
	defer close(gCChan)
	worker.NewGarbageCollector(kubeClient, dynamicClient, gCChan, s.GCIntervalMin*60, time.Minute*30, k8sTAPSpec.IsOpenShift()).StartCleanup()

	glog.V(1).Infof("********** Start running Kubeturbo Service **********")
	k8sTAPService.Run()
	glog.V(1).Info("Kubeturbo service is stopped.")

	cleanupWG.Wait()
	glog.V(1).Info("Cleanup completed. Exiting gracefully.")
}

func (s *VMTServer) startHttp() {
	mux := http.NewServeMux()

	// healthz
	healthz.InstallHandler(mux)

	// debug
	if s.EnableProfiling {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		// prometheus.metrics
		mux.Handle("/metrics", promhttp.Handler())
	}

	server := &http.Server{
		Addr:    net.JoinHostPort(s.Address, strconv.Itoa(s.Port)),
		Handler: mux,
	}
	glog.Fatal(server.ListenAndServe())
}

// handleExit disconnects the tap service from Turbo service when Kubeturbo is shotdown
func handleExit(wg *sync.WaitGroup, cleanUpFns ...cleanUp) { // k8sTAPService *kubeturbo.K8sTAPService) {
	glog.V(4).Infof("*** Handling Kubeturbo Termination ***")
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGHUP)

	wg.Add(1)
	go func() {
		select {
		case sig := <-sigChan:
			glog.V(2).Infof("Signal %s received. Will run exit handlers.. \n", sig)
			for _, f := range cleanUpFns {
				// The default graceful timeout, once a container is sent a SIGTERM before it is
				// killed in k8s is 30 seconds. We want to make maximum use of that time.
				wg.Add(1)
				go func(f cleanUp) {
					f()
					wg.Done()
				}(f)
			}
		}
		wg.Done()
	}()
}

func discoverk8sAPIResourceGV(client *kubernetes.Clientset, resourceName string) (schema.GroupVersion, error) {
	// We optimistically use a globally set default if we cannot discover the GV.
	defaultGV := util.K8sAPIDeploymentReplicasetDefaultGV

	apiResourceLists, err := client.ServerPreferredResources()
	if apiResourceLists == nil {
		return defaultGV, err
	}
	if err != nil {
		// We don't exit here as ServerPreferredResources can return the resource list even with errors.
		glog.Warningf("Error listing api resources: %v", err)
	}

	latestExtensionsVersion := schema.GroupVersion{Group: util.K8sExtensionsGroupName, Version: ""}
	latestAppsVersion := schema.GroupVersion{Group: util.K8sAppsGroupName, Version: ""}
	for _, apiResourceList := range apiResourceLists {
		if len(apiResourceList.APIResources) == 0 {
			continue
		}

		found := false
		for _, apiResource := range apiResourceList.APIResources {
			if apiResource.Name == resourceName {
				found = true
				break
			}
		}
		if found == false {
			continue
		}

		gv, err := schema.ParseGroupVersion(apiResourceList.GroupVersion)
		if err != nil {
			return defaultGV, fmt.Errorf("error parsing GroupVersion: %v", err)
		}

		group := gv.Group
		version := gv.Version
		if group == util.K8sExtensionsGroupName {
			latestExtensionsVersion.Version = latestComparedVersion(version, latestExtensionsVersion.Version)
		} else if group == util.K8sAppsGroupName {
			latestAppsVersion.Version = latestComparedVersion(version, latestAppsVersion.Version)
		}
	}

	if latestAppsVersion.Version != "" {
		return latestAppsVersion, nil
	}
	if latestExtensionsVersion.Version != "" {
		return latestExtensionsVersion, nil
	}
	return defaultGV, nil
}

func latestComparedVersion(newVersion, existingVersion string) string {
	if existingVersion != "" && versionhelper.CompareKubeAwareVersionStrings(newVersion, existingVersion) <= 0 {
		return existingVersion
	}
	return newVersion
}

func ManageSCCs(ns string, dynClient dynamic.Interface, kubeClient kubernetes.Interface) {
	if !reviewSCCAccess(ns, kubeClient) {
		// Skip managing scc resources; appropriate error messages are already logged
		// by the review function
		return
	}

	sccList := GetSCCs(dynClient)
	if (sccList == nil) || (sccList != nil && len(sccList.Items) < 1) {
		// We don't need to bother as this cluster is most probably not openshift
		return
	}
	glog.V(3).Info("This looks like an openshift cluster and kubeturbo has appropriate permissions to manage SCCs.")

	fail := true
	defer func() {
		if fail {
			CleanUpSCCMgmtResources(ns, dynClient, kubeClient)
		}
	}()

	saNames := []string{}
	for _, scc := range sccList.Items {
		sccName := scc.GetName()
		saName, err := createSCCServiceAccount(ns, sccName, kubeClient)
		if err != nil {
			// We have no option but to abort halfway and cleanup in case of persistent errors.
			// We already retry couple of times in case of an error.
			// We also log warning in the called function
			return
		}

		roleName, err := createSCCRole(ns, sccName, kubeClient)
		if err != nil {
			// Warning logged in the called function
			return
		}

		if err := createSCCRoleBinding(saName, ns, sccName, roleName, kubeClient); err != nil {
			// Warning logged in the called function
			return
		}

		// We use this map both for updating the user names in sccs and to cleanup the resources
		// in case of an error or at exit.
		// This has potential for race conditions, for example the service account was created
		// but not updated in this map when the exit was trigerred.
		// Ignoring this as of now because this can be no better then facing transient API errors
		// while deleting resources at exit, which will also leak resources behind.
		// Leaking resources is ok to some extent, because we use constant names and everything is
		// created within a namespace. Any leaked resources will automatically be cleaned up when
		// the kubeturbo namespace is deleted.
		// Also in case of kubeturbo restarts if there are leaked resources, kubeturbo will adopt them.
		util.SCCMapping[sccName] = saName
		saNames = append(saNames, saName)
	}

	clusterRoleName, err := createSCCClusterRole(ns, kubeClient)
	if err != nil {
		return
	}

	if err := createSCCClusterRoleBinding(saNames, ns, clusterRoleName, kubeClient); err != nil {
		return
	}

	fail = false
}

func GetSCCs(client dynamic.Interface) (sccList *unstructured.UnstructuredList) {
	res := schema.GroupVersionResource{
		Group:    util.OpenShiftAPISCCGV.Group,
		Version:  util.OpenShiftAPISCCGV.Version,
		Resource: util.OpenShiftSCCResName,
	}

	err := util.RetryDuring(util.TransientRetryTimes, 0,
		util.QuickRetryInterval, func() error {
			var err error
			sccList, err = client.Resource(res).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				glog.Warningf("Could not get openshift cluster sccs: %v", err)
			}
			return err
		})
	if err != nil {
		return nil
	}
	return sccList
}

func createSCCServiceAccount(namespace, sccName string, kubeClient kubernetes.Interface) (string, error) {
	sa := util.GetServiceAccountForSCC(sccName)
	saName := sa.Name

	// TODO: an improvement on retries would be to retry only on transient errors.
	err := util.RetryDuring(util.TransientRetryTimes, 0,
		util.QuickRetryInterval, func() error {
			_, err := kubeClient.CoreV1().ServiceAccounts(namespace).Create(context.TODO(), sa, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				glog.V(2).Infof("SCC ServiceAccount: %s/%s already exists.", namespace, saName)
				return nil
			}

			if err != nil {
				glog.Errorf("Error creating SCC ServicAccount: %s/%s, %s.", namespace, saName, err)
				return err
			}
			return nil
		})

	return saName, err
}

func createSCCRole(namespace, sccName string, kubeClient kubernetes.Interface) (string, error) {
	role := util.GetRoleForSCC(sccName)
	roleName := role.Name

	err := util.RetryDuring(util.TransientRetryTimes, 0,
		util.QuickRetryInterval, func() error {
			_, err := kubeClient.RbacV1().Roles(namespace).Create(context.TODO(), role, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				glog.V(3).Infof("SCC Role: %s already exists.", roleName)
				return nil
			}

			if err != nil {
				glog.Errorf("Error creating SCC Role: %s, %s.", roleName, err)
				return err
			}

			glog.V(3).Infof("SCC Role: %s created.", roleName)
			return nil
		})

	return roleName, err
}

func createSCCRoleBinding(saName, namespace, sccName, roleName string, kubeClient kubernetes.Interface) error {
	rb := util.GetRoleBindingForSCC(saName, namespace, sccName, roleName)
	return util.RetryDuring(util.TransientRetryTimes, 0,
		util.QuickRetryInterval, func() error {
			_, err := kubeClient.RbacV1().RoleBindings(namespace).Create(context.TODO(), rb, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				// TODO: We ignore the case where a new scc might appear between kubeturbo runs
				// That means a new scc definition will be picked across restarts only.
				glog.V(3).Infof("SCC RoleBinding: %s already exists.", rb.Name)
				return nil
			}

			if err != nil {
				glog.Errorf("Error creating SCC RoleBinding: %s, %s.", rb.Name, err)
				return err
			}

			glog.V(3).Infof("SCC RoleBinding: %s created.", rb.Name)
			return nil
		})
}

func createSCCClusterRole(ns string, kubeClient kubernetes.Interface) (string, error) {
	clusterRole := util.GetClusterRoleForSCC(ns)
	clusterRoleName := clusterRole.Name

	err := util.RetryDuring(util.TransientRetryTimes, 0,
		util.QuickRetryInterval, func() error {
			_, err := kubeClient.RbacV1().ClusterRoles().Create(context.TODO(), clusterRole, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				glog.V(3).Infof("SCC Cluster Role: %s already exists.", clusterRoleName)
				return nil
			}

			if err != nil {
				glog.Errorf("Error creating SCC Cluster Role: %s, %s.", clusterRoleName, err)
				return err
			}

			glog.V(3).Infof("SCC Cluster Role: %s created.", clusterRoleName)
			return nil
		})

	return clusterRoleName, err
}

func createSCCClusterRoleBinding(saNames []string, namespace, roleName string, kubeClient kubernetes.Interface) error {
	crb := util.GetClusterRoleBindingForSCC(saNames, namespace, roleName)
	return util.RetryDuring(util.TransientRetryTimes, 0,
		util.QuickRetryInterval, func() error {
			_, err := kubeClient.RbacV1().ClusterRoleBindings().Create(context.TODO(), crb, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				// TODO: We ignore the case where a new scc might appear between kubeturbo runs
				// That means a new scc definition will be picked across restarts only.
				glog.V(3).Infof("SCC ClusterRoleBinding: %s already exists. "+
					"It will be updated with the latest subjects.", crb.Name)
				_, err := kubeClient.RbacV1().ClusterRoleBindings().Update(context.TODO(), crb, metav1.UpdateOptions{})
				return err
			}

			if err != nil {
				glog.Errorf("Error creating SCC ClusterRoleBinding: %s, %s.", crb.Name, err)
				return err
			}

			glog.V(3).Infof("SCC ClusterRoleBinding: %s created.", crb.Name)
			return nil
		})
}

func CleanUpSCCMgmtResources(ns string, dynClient dynamic.Interface, kubeClient kubernetes.Interface) {
	if len(util.SCCMapping) < 1 {
		glog.V(2).Infof("SCC management resource cleanup is not needed.")
		return
	}
	glog.V(2).Infof("SCC management resource cleanup started.")

	for sccName, saName := range util.SCCMapping {
		// Errors are found and logged in the called methods.
		// We retry couple of times on errors but continue to delete other
		// resources on persistent error on a particular resource.
		deleteSCCRole(ns, sccName, kubeClient)
		deleteSCCRoleBinding(ns, sccName, kubeClient)
		if err := util.RetryDuring(util.TransientRetryTimes, 0,
			util.QuickRetryInterval, func() error {
				err := kubeClient.CoreV1().ServiceAccounts(ns).Delete(context.TODO(), saName, metav1.DeleteOptions{})
				if apierrors.IsNotFound(err) {
					glog.V(2).Infof("SCC ServiceAccount: %s/%s already deleted.", ns, saName)
					return nil
				}

				if err != nil {
					glog.Errorf("Error deleting SCC ServicAccount: %s/%s, %s.", ns, saName, err)
					return err
				}

				return nil
			}); err != nil {
			glog.Error(err)
		}

	}

	// Errors are found and logged in the called methods.
	deleteSCCClusterRoleBinding(ns, kubeClient)
	deleteSCCClusterRole(ns, kubeClient)
	glog.V(2).Infof("SCC management resource cleanup completed.")
}

func deleteSCCRole(namespace, sccName string, kubeClient kubernetes.Interface) {
	roleName := util.RoleNameForSCC(sccName)
	err := util.RetryDuring(util.TransientRetryTimes, 0,
		util.QuickRetryInterval, func() error {
			err := kubeClient.RbacV1().Roles(namespace).Delete(context.TODO(), roleName, metav1.DeleteOptions{})
			if apierrors.IsNotFound(err) {
				glog.V(3).Infof("SCC Role: %s already deleted.", roleName)
				return nil
			}

			if err != nil {
				glog.Errorf("Error deleting SCC Role: %s, %s.", roleName, err)
				return err
			}
			return nil
		})
	if err != nil {
		glog.Errorf("Error deleting SCC role. %v", err)
	}
}

func deleteSCCRoleBinding(namespace, sccName string, kubeClient kubernetes.Interface) {
	roleBindingName := util.RoleBindingNameForSCC(sccName)
	err := util.RetryDuring(util.TransientRetryTimes, 0,
		util.QuickRetryInterval, func() error {
			err := kubeClient.RbacV1().RoleBindings(namespace).Delete(context.TODO(), roleBindingName, metav1.DeleteOptions{})
			if apierrors.IsNotFound(err) {
				glog.V(3).Infof("SCC RoleBinding: %s already deleted.", roleBindingName)
			}

			if err != nil {
				glog.Errorf("Error deleting SCC RoleBinding: %s, %s.", roleBindingName, err)
			}
			return nil
		})
	if err != nil {
		glog.Errorf("Error deleting SCC role binding. %v", err)
	}
}

func deleteSCCClusterRole(namespace string, kubeClient kubernetes.Interface) {
	clusterRoleName := fmt.Sprintf("%s-%s", util.SCCClusterRoleName, namespace)
	err := util.RetryDuring(util.TransientRetryTimes, 0,
		util.QuickRetryInterval, func() error {
			err := kubeClient.RbacV1().ClusterRoles().Delete(context.TODO(), clusterRoleName, metav1.DeleteOptions{})
			if apierrors.IsNotFound(err) {
				glog.V(3).Infof("SCC ClusterRole: %s already deleted.", clusterRoleName)
				return nil
			}

			if err != nil {
				glog.Errorf("Error deleting SCC ClusterRole: %s, %s.", clusterRoleName, err)
				return err
			}
			return nil
		})
	if err != nil {
		glog.Errorf("Error deleting SCC cluster role. %v", err)
	}
}

func deleteSCCClusterRoleBinding(namespace string, kubeClient kubernetes.Interface) {
	clusterRoleBindingName := fmt.Sprintf("%s-%s", util.SCCClusterRoleBindingName, namespace)
	err := util.RetryDuring(util.TransientRetryTimes, 0,
		util.QuickRetryInterval, func() error {
			err := kubeClient.RbacV1().ClusterRoleBindings().Delete(context.TODO(), clusterRoleBindingName, metav1.DeleteOptions{})
			if apierrors.IsNotFound(err) {
				glog.V(3).Infof("SCC ClusterRoleBinding: %s already deleted.", clusterRoleBindingName)
			}

			if err != nil {
				glog.Errorf("Error deleting SCC ClusterRoleBinding: %s, %s.", clusterRoleBindingName, err)
			}
			return nil
		})
	if err != nil {
		glog.Errorf("Error deleting SCC cluster role binding. %v", err)
	}
}

// reviewSCCAccess checks the permissions for resources that are needed
// to be created or altered for SCC level functionality.
func reviewSCCAccess(namespace string, kubeClient kubernetes.Interface) bool {
	for _, review := range util.GetSelfSubjectAccessReviews(namespace) {
		permission, err := kubeClient.AuthorizationV1().SelfSubjectAccessReviews().Create(context.TODO(), &review, metav1.CreateOptions{})
		if err != nil {
			glog.Errorf("Error reviewing kubeturbo permissions: %v. Kubeturbo cannot"+
				"use appropriate SCC levels while restarting pods.", err)
			return false
		}
		if permission.Status.Allowed != true {
			glog.Errorf("Kubeturbo does not have \"%s\" permission for \"%s\". Kubeturbo cannot"+
				"use appropriate SCC levels while restarting pods.", review.Spec.ResourceAttributes.Verb,
				review.Spec.ResourceAttributes.Resource)
			return false
		}
	}

	return true
}

func WatchConfigMap() {
	//Check if the file /etc/kubeturbo/turbo-autoreload.config exists
	autoReloadConfigFilePath := "/etc/kubeturbo"
	autoReloadConfigFileName := "turbo-autoreload.config"
	retrySeconds := 30

	viper.AddConfigPath(autoReloadConfigFilePath)
	viper.SetConfigType("json")
	viper.SetConfigName(autoReloadConfigFileName)
	for {
		if verr := viper.ReadInConfig(); verr == nil {
			break
		} else {
			if _, ok := verr.(viper.ConfigFileNotFoundError); ok {
				glog.V(4).Infof("Autoreload config file %s/%s not found", autoReloadConfigFilePath, autoReloadConfigFileName)
			} else {
				glog.Warningf("Invalid configuration: error %s reading autoreload config %s/%s. Retry in %d seconds.",
					verr, autoReloadConfigFilePath, autoReloadConfigFileName, retrySeconds)
			}
			time.Sleep(time.Duration(retrySeconds) * time.Second)
		}
	}

	currentMinNodes := cluster.DefaultMinNodePoolSize
	currentMaxNodes := cluster.DefaultMaxNodePoolSize

	glog.V(1).Infof("Start watching the autoreload config file %s/%s", autoReloadConfigFilePath, autoReloadConfigFileName)
	updateConfigClosure := func() {
		updateLoggingLevel()
		updateNodePoolConfig(&currentMinNodes, &currentMaxNodes)
	}
	updateConfigClosure() //update the logging level during startup
	viper.OnConfigChange(func(in fsnotify.Event) {
		updateConfigClosure()
	})

	viper.WatchConfig()
}

// updateLoggingLevel updates the logging verbosity level based on configuration.
func updateLoggingLevel() {
	newLoggingLevel := viper.GetString("logging.level")
	currentLoggingLevel := pflag.Lookup("v").Value.String()

	if newLoggingLevel != "" && newLoggingLevel != currentLoggingLevel {
		if newLogVInt, err := strconv.Atoi(newLoggingLevel); err != nil || newLogVInt < 0 {
			glog.Errorf("Invalid log verbosity %v in the autoreload config file", newLoggingLevel)
		} else {
			err := pflag.Lookup("v").Value.Set(newLoggingLevel)
			if err != nil {
				glog.Errorf("Can't apply the new logging level setting due to the error:%v", err)
			} else {
				glog.V(1).Infof("Logging level is changed from %v to %v", currentLoggingLevel, newLoggingLevel)
			}
		}
	}
}

func updateNodePoolConfig(currentMinNodes *int, currentMaxNodes *int) {
	newMinNodes := logCurrentValueAndGetValue(cluster.MinNodesConfigKey, *currentMinNodes, cluster.DefaultMinNodePoolSize)
	newMaxNodes := logCurrentValueAndGetValue(cluster.MaxNodesConfigKey, *currentMaxNodes, cluster.DefaultMaxNodePoolSize)

	if newMinNodes > newMaxNodes {
		glog.Errorf("Cannot make cluster node pool size configuration change since %s value: %d is larger than %s value: %d",
			cluster.MinNodesConfigKey, newMinNodes, cluster.MaxNodesConfigKey, newMaxNodes)
	} else {
		updateValueAndLog(cluster.MinNodesConfigKey, currentMinNodes, newMinNodes)
		updateValueAndLog(cluster.MaxNodesConfigKey, currentMaxNodes, newMaxNodes)
	}
}

func logCurrentValueAndGetValue(configKey string, currentValue int, defaultValue int) int {
	glog.V(1).Infof("Cluster %s current value is %v", configKey, currentValue)
	return cluster.GetNodePoolSizeConfigValue(configKey, viper.GetString, defaultValue)
}

func updateValueAndLog(configKey string, currentValue *int, newValue int) {
	if *currentValue != newValue {
		glog.V(1).Infof("Cluster %s changed from %v to %v", configKey, *currentValue, newValue)
		*currentValue = newValue
	}
}
