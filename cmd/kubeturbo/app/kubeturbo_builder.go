package app

import (
	"net"
	"os"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/apis/componentconfig"
	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/internalclientset"
	"k8s.io/kubernetes/pkg/client/leaderelection"
	"k8s.io/kubernetes/pkg/client/record"
	"k8s.io/kubernetes/pkg/client/restclient"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/client/unversioned/clientcmd"
	_ "k8s.io/kubernetes/plugin/pkg/scheduler/algorithmprovider"

	kubeturbo "github.com/turbonomic/kubeturbo/pkg"
	"github.com/turbonomic/kubeturbo/pkg/discovery/probe"
	"github.com/turbonomic/kubeturbo/test/flag"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
	"github.com/turbonomic/kubeturbo/pkg/turbostore"
)

const (
	// The default port for vmt service server
	VMTPort = 10265

	DefaultEtcdPathPrefix = "/registry"
)

// VMTServer has all the context and params needed to run a Scheduler
type VMTServer struct {
	Port                  int
	Address               net.IP
	Master                string
	K8sTAPSpec            string
	TestingFlagPath       string
	Kubeconfig            string
	BindPodsQPS           float32
	BindPodsBurst         int
	CadvisorPort          int
	EtcdServerList        []string
	EtcdCA                string
	EtcdClientCertificate string
	EtcdClientKey         string
	EtcdConfigFile        string
	EtcdPathPrefix        string

	TurboServerAddress string
	TurboServerPort    string
	OpsManagerUsername string
	OpsManagerPassword string

	LeaderElection componentconfig.LeaderElectionConfiguration
}

// NewVMTServer creates a new VMTServer with default parameters
func NewVMTServer() *VMTServer {
	s := VMTServer{
		Port:    VMTPort,
		Address: net.ParseIP("127.0.0.1"),
	}
	return &s
}

// AddFlags adds flags for a specific VMTServer to the specified FlagSet
func (s *VMTServer) AddFlags(fs *pflag.FlagSet) {
	fs.IntVar(&s.Port, "port", s.Port, "The port that the kubeturbo's http service runs on")
	fs.IntVar(&s.CadvisorPort, "cadvisor-port", 4194, "The port of the cadvisor service runs on")
	fs.StringVar(&s.Master, "master", s.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	fs.StringVar(&s.K8sTAPSpec, "config-path", s.K8sTAPSpec, "The path to the config file.")
	fs.StringVar(&s.TestingFlagPath, "flag-path", s.TestingFlagPath, "The path to the testing flag.")
	fs.StringVar(&s.Kubeconfig, "kubeconfig", s.Kubeconfig, "Path to kubeconfig file with authorization and master location information.")
	fs.StringVar(&s.TurboServerAddress, "serveraddress", s.TurboServerAddress, "Address of Turbo Server")
	fs.StringVar(&s.TurboServerPort, "serverport", "", "Port of Turbo Server")
	fs.StringVar(&s.OpsManagerUsername, "opsmanagerusername", s.OpsManagerUsername, "Username for Ops Manager")
	fs.StringVar(&s.OpsManagerPassword, "opsmanagerpassword", s.OpsManagerPassword, "Password for Ops Manager")
	leaderelection.BindFlags(&s.LeaderElection, fs)
}

// Run runs the specified VMTServer.  This should never exit.
func (s *VMTServer) Run(_ []string) error {
	if s.Kubeconfig == "" && s.Master == "" {
		glog.Warningf("Neither --kubeconfig nor --master was specified.  Using default API client.  This might not work.")
	}

	glog.V(3).Infof("Master is %s", s.Master)

	if s.TestingFlagPath != "" {
		flag.SetPath(s.TestingFlagPath)
	}

	if s.CadvisorPort == 0 {
		s.CadvisorPort = 4194
	}

	probeConfig := &probe.ProbeConfig{
		CadvisorPort: s.CadvisorPort,
	}
	// This creates a client, first loading any specified kubeconfig
	// file, and then overriding the Master flag, if non-empty.
	// kubeconfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
	// 	&clientcmd.ClientConfigLoadingRules{ExplicitPath: s.Kubeconfig},
	// 	&clientcmd.ConfigOverrides{ClusterInfo: clientcmdapi.Cluster{Server: s.Master}}).ClientConfig()

	kubeConfig, err := clientcmd.BuildConfigFromFlags(s.Master, s.Kubeconfig)
	if err != nil {
		glog.Errorf("Error getting kubeconfig:  %s", err)
		return err
	}
	// This specifies the number and the max number of query per second to the api server.
	kubeConfig.QPS = 20.0
	kubeConfig.Burst = 30

	kubeClient, err := client.New(kubeConfig)
	if err != nil {
		glog.Fatalf("Invalid API configuration: %v", err)
	}

	leaderElectionClient, err := clientset.NewForConfig(restclient.AddUserAgent(kubeConfig, "leader-election"))
	if err != nil {
		glog.Fatalf("Invalid API configuration: %v", err)
	}

	glog.V(3).Infof("spec path is: %v", s.K8sTAPSpec)

	k8sTAPSpec, err := kubeturbo.ParseK8sTAPServiceSpec(s.K8sTAPSpec)
	if err != nil {
		glog.Errorf("Failed to generate correct TAP config: %s", err)
		os.Exit(1)
	}
	broker := turbostore.NewPodBroker()
	vmtConfig := kubeturbo.NewVMTConfig(kubeClient, probeConfig, broker, k8sTAPSpec)
	glog.V(3).Infof("Finished creating turbo configuration: %++v", vmtConfig)

	eventBroadcaster := record.NewBroadcaster()
	vmtConfig.Recorder = eventBroadcaster.NewRecorder(api.EventSource{Component: "kubeturbo"})
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(kubeClient.Events(""))

	vmtService := kubeturbo.NewKubeturboService(vmtConfig)

	run := func(_ <-chan struct{}) {
		vmtService.Run()
		select {}
	}

	if !s.LeaderElection.LeaderElect {
		glog.Infof("No leader election")
		run(nil)
		glog.Fatal("this statement is unreachable")
		panic("unreachable")
	}

	id, err := os.Hostname()
	if err != nil {
		return err
	}

	leaderelection.RunOrDie(leaderelection.LeaderElectionConfig{
		EndpointsMeta: api.ObjectMeta{
			Namespace: "kube-system",
			Name:      "kubeturbo",
		},
		Client:        leaderElectionClient,
		Identity:      id,
		EventRecorder: vmtConfig.Recorder,
		LeaseDuration: s.LeaderElection.LeaseDuration.Duration,
		RenewDeadline: s.LeaderElection.RenewDeadline.Duration,
		RetryPeriod:   s.LeaderElection.RetryPeriod.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: run,
			OnStoppedLeading: func() {
				glog.Fatalf("lost master")
			},
		},
	})

	glog.Fatal("this statement is unreachable")
	panic("unreachable")
}
