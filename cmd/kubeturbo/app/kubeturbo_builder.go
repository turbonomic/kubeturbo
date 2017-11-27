package app

import (
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"

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
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/k8sconntrack"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/kubelet"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/master"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"
	"github.com/turbonomic/kubeturbo/test/flag"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/pflag"
)

const (
	// The default port for vmt service server
	KubeturboPort   = 10265
	K8sCadvisorPort = 4194
)

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
	CAdvisorPort    int

	//LeaderElection componentconfig.LeaderElectionConfiguration

	EnableProfiling bool

	// If the underlying infrastructure is VMWare, we cannot reply on IP address for stitching. Instead we use the
	// systemUUID of each node, which is equal to UUID of corresponding VM discovered by VM probe.
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
	fs.IntVar(&s.CAdvisorPort, "cadvisor-port", K8sCadvisorPort, "The port of the cadvisor service runs on")
	fs.StringVar(&s.Master, "master", s.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	fs.StringVar(&s.K8sTAPSpec, "turboconfig", s.K8sTAPSpec, "Path to the config file.")
	fs.StringVar(&s.TestingFlagPath, "testingflag", s.TestingFlagPath, "Path to the testing flag.")
	fs.StringVar(&s.KubeConfig, "kubeconfig", s.KubeConfig, "Path to kubeconfig file with authorization and master location information.")
	fs.BoolVar(&s.EnableProfiling, "profiling", false, "Enable profiling via web interface host:port/debug/pprof/.")
	fs.BoolVar(&s.UseVMWare, "usevmware", false, "If the underlying infrastructure is VMWare.")
	fs.IntVar(&s.KubeletPort, "kubelet-port", kubelet.DefaultKubeletPort, "The port of the kubelet runs on")
	fs.BoolVar(&s.EnableKubeletHttps, "kubelet-https", kubelet.DefaultKubeletHttps, "Indicate if Kubelet is running on https server")
	fs.StringVar(&s.K8sVersion, "k8sVersion", executor.HigherK8sVersion, "the kubernetes server version; for openshift, it is the underlying Kubernetes' version.")
	fs.StringVar(&s.NoneSchedulerName, "noneSchedulerName", executor.DefaultNoneExistSchedulerName, "a none-exist scheduler name, to prevent controller to create Running pods during move Action.")
	fs.BoolVar(&s.enableNonDisruptiveSupport, "enable-non-disruptive-support", false, "Indicate if nondisruptive action support is enabled")

	//leaderelection.BindFlags(&s.LeaderElection, fs)
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

func (s *VMTServer) createKubeletClientOrDie(kubeConfig *restclient.Config) *kubelet.KubeletClient {
	kubeletClient, err := kubelet.NewKubeletConfig(kubeConfig).
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

func (s *VMTServer) createProbeConfigOrDie(kubeConfig *restclient.Config, kubeletClient *kubelet.KubeletClient) *configs.ProbeConfig {
	// The default property type for stitching is IP.
	pType := stitching.IP
	if s.UseVMWare {
		// If the underlying hypervisor is vCenter, use UUID.
		// Refer to Bug: https://vmturbo.atlassian.net/browse/OM-18139
		pType = stitching.UUID
	}

	// Create Kubelet monitoring
	kubeletMonitoringConfig := kubelet.NewKubeletMonitorConfig(kubeletClient)

	// Create cluster monitoring
	masterMonitoringConfig, err := master.NewClusterMonitorConfig(kubeConfig)
	if err != nil {
		glog.Errorf("Failed to build monitor-config for master topology mointor: %v", err)
		os.Exit(1)
	}

	// TODO for now kubelet is the only monitoring source. As we have more sources, we should choose what to be added into the slice here.
	monitoringConfigs := []monitoring.MonitorWorkerConfig{
		kubeletMonitoringConfig,
		masterMonitoringConfig,
	}

	// Create K8sConntrack monitoring
	// TODO, disable https by default. Change this when k8sconntrack supports https.
	k8sConntrackMonitoringConfig := k8sconntrack.NewK8sConntrackMonitorConfig()
	monitoringConfigs = append(monitoringConfigs, k8sConntrackMonitoringConfig)

	probeConfig := &configs.ProbeConfig{
		CadvisorPort:          s.CAdvisorPort,
		StitchingPropertyType: pType,
		MonitoringConfigs:     monitoringConfigs,
	}

	return probeConfig
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

	glog.V(3).Infof("spec path is: %v", s.K8sTAPSpec)
	k8sTAPSpec, err := kubeturbo.ParseK8sTAPServiceSpec(s.K8sTAPSpec)
	if err != nil {
		glog.Errorf("Failed to generate correct TAP config: %v", err.Error())
		os.Exit(1)
	}

	kubeConfig := s.createKubeConfigOrDie()
	kubeClient := s.createKubeClientOrDie(kubeConfig)
	kubeletClient := s.createKubeletClientOrDie(kubeConfig)
	probeConfig := s.createProbeConfigOrDie(kubeConfig, kubeletClient)
	broker := turbostore.NewPodBroker()

	vmtConfig := kubeturbo.NewVMTConfig2()
	vmtConfig.WithTapSpec(k8sTAPSpec).
		WithKubeClient(kubeClient).
		WithKubeletClient(kubeletClient).
		WithProbeConfig(probeConfig).
		WithBroker(broker).
		WithK8sVersion(s.K8sVersion).
		WithNoneScheduler(s.NoneSchedulerName).
		WithRecorder(createRecorder(kubeClient)).
		WithEnableNonDisruptiveFlag(s.enableNonDisruptiveSupport)
	glog.V(3).Infof("Finished creating turbo configuration: %+v", vmtConfig)

	vmtService := kubeturbo.NewKubeturboService(vmtConfig)
	run := func(_ <-chan struct{}) {
		vmtService.Run()
		select {}
	}

	go s.startHttp()

	//if !s.LeaderElection.LeaderElect {
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
