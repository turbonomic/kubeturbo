package app

import (
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	apiv1 "k8s.io/client-go/pkg/api/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"

	"github.com/turbonomic/kubeturbo/pkg"
	"github.com/turbonomic/kubeturbo/test/flag"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/pflag"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
)

const (
	// The default port for vmt service server
	KubeturboPort               = 10265
	DefaultKubeletPort          = 10255
	DefaultKubeletHttps         = false
	defaultVMPriority           = -1
	defaultVMIsBase             = true
	defaultDiscoveryIntervalSec = 600
	defaultValidationWorkers    = 10
	defaultValidationTimeout    = 60
)

var (
	//these variables will be deprecated. Keep it here for backward compatibility only
	k8sVersion        = "1.8"
	noneSchedulerName = "turbo-no-scheduler"
)

type disconnectFromTurboFunc func()

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

	//LeaderElection componentconfig.LeaderElectionConfiguration

	EnableProfiling bool

	// To stitch the Nodes in Kubernetes cluster with the VM from the underlying cloud or
	// hypervisor infrastructure: either use VM UUID or VM IP.
	// If the underlying infrastructure is VMWare, AWS instances, or Azure instances, VM's UUID is used.
	UseUUID bool

	//VMPriority: priority of VM in supplyChain definition from kubeturbo, should be less than 0;
	VMPriority int32
	//VMIsBase: Is VM is the base template from kubeturbo, when stitching with other VM probes, should be false;
	VMIsBase bool

	// Kubelet related config
	KubeletPort        int
	EnableKubeletHttps bool

	// The cluster processor related config
	ValidationWorkers int
	ValidationTimeout int
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
	fs.IntVar(&s.Port, "port", s.Port, "The port that kubeturbo's http service runs on")
	fs.StringVar(&s.Address, "ip", s.Address, "the ip address that kubeturbo's http service runs on")
	fs.StringVar(&s.Master, "master", s.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	fs.StringVar(&s.K8sTAPSpec, "turboconfig", s.K8sTAPSpec, "Path to the config file.")
	fs.StringVar(&s.TestingFlagPath, "testingflag", s.TestingFlagPath, "Path to the testing flag.")
	fs.StringVar(&s.KubeConfig, "kubeconfig", s.KubeConfig, "Path to kubeconfig file with authorization and master location information.")
	fs.BoolVar(&s.EnableProfiling, "profiling", false, "Enable profiling via web interface host:port/debug/pprof/.")
	fs.BoolVar(&s.UseUUID, "stitch-uuid", true, "Use VirtualMachine's UUID to do stitching, otherwise IP is used.")
	fs.IntVar(&s.KubeletPort, "kubelet-port", DefaultKubeletPort, "The port of the kubelet runs on")
	fs.BoolVar(&s.EnableKubeletHttps, "kubelet-https", DefaultKubeletHttps, "Indicate if Kubelet is running on https server")
	fs.StringVar(&k8sVersion, "k8sVersion", k8sVersion, "[deprecated] the kubernetes server version; for openshift, it is the underlying Kubernetes' version.")
	fs.StringVar(&noneSchedulerName, "noneSchedulerName", noneSchedulerName, "[deprecated] a none-exist scheduler name, to prevent controller to create Running pods during move Action.")
	fs.IntVar(&s.DiscoveryIntervalSec, "discovery-interval-sec", defaultDiscoveryIntervalSec, "The discovery interval in seconds")
	fs.IntVar(&s.ValidationWorkers, "validation-workers", defaultValidationWorkers, "The validation workers")
	fs.IntVar(&s.ValidationTimeout, "validation-timeout-sec", defaultValidationTimeout, "The validation timeout in seconds")
}

// create an eventRecorder to send events to Kubernetes APIserver
func createRecorder(kubecli *kubernetes.Clientset) record.EventRecorder {
	// Create a new broadcaster which will send events we generate to the apiserver
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: v1core.New(kubecli.Core().RESTClient()).Events(apiv1.NamespaceAll)})
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

func (s *VMTServer) createKubeletClientOrDie(kubeConfig *restclient.Config, allowTLSInsecure bool) *kubeclient.KubeletClient {
	kubeletClient, err := kubeclient.NewKubeletConfig(kubeConfig).
		WithPort(s.KubeletPort).
		EnableHttps(s.EnableKubeletHttps).
		AllowTLSInsecure(allowTLSInsecure).
		//Timeout(to).
		Create()
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
func (s *VMTServer) Run(_ []string) error {
	if err := s.checkFlag(); err != nil {
		glog.Errorf("check flag failed:%v. abort.", err.Error())
		os.Exit(1)
	}

	kubeConfig := s.createKubeConfigOrDie()
	glog.V(3).Infof("kubeConfig: %+v", kubeConfig)

	kubeClient := s.createKubeClientOrDie(kubeConfig)

	isOpenshift := checkServerVersion(kubeClient.DiscoveryClient.RESTClient())
	glog.V(2).Info("Openshift cluster? ", isOpenshift)

	// Allow insecure connection only if it's not an Openshift cluster
	// For Kubernetes distro, the secure connection to Kubelet will fail due to
	// the certificate issue of 'doesn't contain any IP SANs'.
	// See https://github.com/kubernetes/kubernetes/issues/59372
	kubeletClient := s.createKubeletClientOrDie(kubeConfig, !isOpenshift)

	glog.V(3).Infof("spec path is: %v", s.K8sTAPSpec)
	k8sTAPSpec, err := kubeturbo.ParseK8sTAPServiceSpec(s.K8sTAPSpec, kubeConfig.Host)
	if err != nil {
		glog.Errorf("Failed to generate correct TAP config: %v", err.Error())
		os.Exit(1)
	}

	// Configuration for creating the Kubeturbo TAP service
	vmtConfig := kubeturbo.NewVMTConfig2()
	vmtConfig.WithTapSpec(k8sTAPSpec).
		WithKubeClient(kubeClient).
		WithKubeletClient(kubeletClient).
		WithVMPriority(s.VMPriority).
		WithVMIsBase(s.VMIsBase).
		UsingUUIDStitch(s.UseUUID).
		WithDiscoveryInterval(s.DiscoveryIntervalSec).
		WithValidationTimeout(s.ValidationTimeout).
		WithValidationWorkers(s.ValidationWorkers)
	glog.V(3).Infof("Finished creating turbo configuration: %+v", vmtConfig)

	// The KubeTurbo TAP service
	k8sTAPService, err := kubeturbo.NewKubernetesTAPService(vmtConfig)
	if err != nil {
		glog.Fatalf("Unexpected error while creating Kuberntes TAP service: %s", err)
	}

	// The client for healthz, debug, and prometheus
	go s.startHttp()
	glog.V(2).Infof("No leader election")

	glog.V(1).Infof("********** Start runnning Kubeturbo Service **********")
	// Disconnect from Turbo server when Kubeturbo is shutdown
	handleExit(func() { k8sTAPService.DisconnectFromTurbo() })
	k8sTAPService.ConnectToTurbo()

	glog.V(1).Info("Kubeturbo service is stopped.")
	return nil
}

func (s *VMTServer) startHttp() {
	mux := http.NewServeMux()

	//healthz
	healthz.InstallHandler(mux)

	//debug
	if s.EnableProfiling {
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		//prometheus.metrics
		mux.Handle("/metrics", prometheus.Handler())
	}

	server := &http.Server{
		Addr:    net.JoinHostPort(s.Address, strconv.Itoa(s.Port)),
		Handler: mux,
	}
	glog.Fatal(server.ListenAndServe())
}

// handleExit disconnects the tap service from Turbo service when Kubeturbo is shotdown
func handleExit(disconnectFunc disconnectFromTurboFunc) { //k8sTAPService *kubeturbo.K8sTAPService) {
	glog.V(4).Infof("*** Handling Kubeturbo Termination ***")
	sigChan := make(chan os.Signal)
	signal.Notify(sigChan,
		os.Interrupt,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGHUP)

	go func() {
		select {
		case sig := <-sigChan:
			// Close the mediation container including the endpoints. It avoids the
			// invalid endpoints remaining in the server side. See OM-28801.
			glog.V(2).Infof("Signal %s received. Disconnecting from Turbo server...\n", sig)
			disconnectFunc()
		}
	}()
}

// checkServerVersion checks and logs the server version and return if it is Openshift distro
func checkServerVersion(restClient restclient.Interface) bool {
	// Check Kubernetes version
	bytes, err := restClient.Get().AbsPath("/version").DoRaw()
	if err != nil {
		glog.Error("Unable to get Kubernetes version info: %v", err)
		return false
	}
	glog.V(2).Info("Kubernetes version: ", string(bytes))

	// Check Openshift version, if exists
	if bytes, err = restClient.Get().AbsPath("/version/openshift").DoRaw(); err == nil {
		glog.V(2).Info("Openshift version: ", string(bytes))
		return true
	}

	return false
}
