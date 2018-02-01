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

	kubeturbo "github.com/turbonomic/kubeturbo/pkg"
	"github.com/turbonomic/kubeturbo/pkg/action/executor"
	"github.com/turbonomic/kubeturbo/test/flag"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/pflag"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
)

const (
	// The default port for vmt service server
	KubeturboPort       = 10265
	K8sCadvisorPort     = 4194
	DefaultKubeletPort  = 10255
	DefaultKubeletHttps = false
)

type disconnectFromTurboFunc func()

// VMTServer has all the context and params needed to run a Scheduler
// TODO: leaderElection is disabled now because of dependency problems.
type VMTServer struct {
	Port            int
	Address         string
	Master          string
	K8sTAPSpec      string
	TestingFlagPath string
	KubeConfig      string
	BindPodsQPS     float32
	BindPodsBurst   int

	//LeaderElection componentconfig.LeaderElectionConfiguration

	EnableProfiling bool

	// To stitch the Nodes in Kubernetes cluster with the VM from the underlying cloud or
	// hypervisor infrastructure, Node IP is used.
	// If the underlying infrastructure is VMWare, we cannot reply on IP address for stitching.
	// Instead we use the systemUUID of each node, which is equal to UUID of corresponding
	// VM discovered by VM probe.
	// The default value is false.
	UseVMWare bool

	// Kubelet related config
	KubeletPort        int
	EnableKubeletHttps bool

	// for Move Action
	K8sVersion        string
	NoneSchedulerName string

	// Flag for supporting non-disruptive action
	enableNonDisruptiveSupport bool
}

// NewVMTServer creates a new VMTServer with default parameters
func NewVMTServer() *VMTServer {
	s := VMTServer{
		Port:    KubeturboPort,
		Address: "127.0.0.1",
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
	fs.BoolVar(&s.UseVMWare, "usevmware", false, "If the underlying infrastructure is VMWare.")
	fs.IntVar(&s.KubeletPort, "kubelet-port", DefaultKubeletPort, "The port of the kubelet runs on")
	fs.BoolVar(&s.EnableKubeletHttps, "kubelet-https", DefaultKubeletHttps, "Indicate if Kubelet is running on https server")
	fs.StringVar(&s.K8sVersion, "k8sVersion", executor.HigherK8sVersion, "the kubernetes server version; for openshift, it is the underlying Kubernetes' version.")
	fs.StringVar(&s.NoneSchedulerName, "noneSchedulerName", executor.DefaultNoneExistSchedulerName, "a none-exist scheduler name, to prevent controller to create Running pods during move Action.")
	fs.BoolVar(&s.enableNonDisruptiveSupport, "enable-non-disruptive-support", false, "Indicate if nondisruptive action support is enabled")

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

func (s *VMTServer) createKubeletClientOrDie(kubeConfig *restclient.Config) *kubeclient.KubeletClient {
	kubeletClient, err := kubeclient.NewKubeletConfig(kubeConfig).
		WithPort(s.KubeletPort).
		EnableHttps(s.EnableKubeletHttps).
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
	kubeletClient := s.createKubeletClientOrDie(kubeConfig)

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
		UsingVMWare(s.UseVMWare).
		WithK8sVersion(s.K8sVersion).
		WithNoneScheduler(s.NoneSchedulerName).
		WithEnableNonDisruptiveFlag(s.enableNonDisruptiveSupport)
	glog.V(3).Infof("Finished creating turbo configuration: %+v", vmtConfig)

	// The KubeTurbo TAP service
	k8sTAPService, err := kubeturbo.NewKubernetesTAPService(vmtConfig)
	if err != nil {
		glog.Fatalf("Unexpected error while creating Kuberntes TAP service: %s", err)
	}

	// Start the KubeTurbo TAP service
	run := func(_ <-chan struct{}) {
		glog.V(2).Infof("********** Start runnning Kubeturbo Service **********")

		// Disconnect from Turbo server when Kubeturbo is shutdown
		handleExit(func() { k8sTAPService.DisconnectFromTurbo() })

		k8sTAPService.ConnectToTurbo()
		select {}
	}

	// The client for healthz, debug, and prometheus
	go s.startHttp()

	glog.V(2).Infof("No leader election")
	run(nil)

	glog.Fatal("this statement is unreachable")
	panic("unreachable")
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
