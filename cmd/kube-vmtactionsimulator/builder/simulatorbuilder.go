package builder

import (
	// "fmt"
	"net"

	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/client/unversioned/clientcmd"

	"github.com/vmturbo/kubeturbo/pkg/conversion"
	"github.com/vmturbo/kubeturbo/pkg/registry"
	"github.com/vmturbo/kubeturbo/pkg/storage"
	etcdhelper "github.com/vmturbo/kubeturbo/pkg/storage/etcd"

	etcdclient "github.com/coreos/etcd/client"
	"github.com/golang/glog"
	"github.com/spf13/pflag"
)

const (
	VMTPort               = 10265
	DefaultEtcdPathPrefix = "/registry"
)

type ActionSimulator struct {
	kubeClient         *client.Client
	etcdStorage        storage.Storage
	action             string
	destination        string
	namespace          string
	pod                string
	application        string
	virtualApplication string
	label              string
	newReplica         string
}

func (as *ActionSimulator) KubeClient() *client.Client {
	return as.kubeClient
}

func (as *ActionSimulator) Etcd() storage.Storage {
	return as.etcdStorage
}

func (as *ActionSimulator) Action() string {
	return as.action
}

func (as *ActionSimulator) Destination() string {
	return as.destination
}

func (as *ActionSimulator) Namespace() string {
	return as.namespace
}

func (as *ActionSimulator) Pod() string {
	return as.pod
}

func (as *ActionSimulator) Label() string {
	return as.label
}

func (as *ActionSimulator) NewReplica() string {
	return as.newReplica
}

func (as *ActionSimulator) Application() string {
	return as.application
}

func (as *ActionSimulator) VirtualApplication() string {
	return as.virtualApplication
}

// VMTServer has all the context and params needed to run a Scheduler
type SimulatorBuilder struct {
	KubeClient            *client.Client
	Port                  int
	Address               net.IP
	Master                string
	Kubeconfig            string
	Namespace             string
	Pod                   string
	Application           string
	VirtualApplication    string
	Label                 string
	NewReplica            string
	Action                string
	Destination           string
	EtcdServerList        []string
	EtcdCA                string
	EtcdClientCertificate string
	EtcdClientKey         string
	EtcdConfigFile        string
	EtcdPathPrefix        string
}

//  creates a new VMTServer with default parameters
func NewSimulatorBuilder() *SimulatorBuilder {
	s := SimulatorBuilder{
		Port:    VMTPort,
		Address: net.ParseIP("127.0.0.1"),
	}
	return &s
}

func (s *SimulatorBuilder) Build() (*ActionSimulator, error) {
	etcdclientBuilder := etcdhelper.NewEtcdClientBuilder().ServerList(s.EtcdServerList).SetTransport(s.EtcdCA, s.EtcdClientCertificate, s.EtcdClientKey)
	etcdClient, err := etcdclientBuilder.CreateAndTest()
	if err != nil {
		glog.Errorf("Error creating etcd client instance for vmt service: %s", err)
		return nil, err
	}
	etcdStorage, err := newEtcd(etcdClient, s.EtcdPathPrefix)

	if err != nil {
		glog.Warningf("Error creating etcd storage instance for vmt service: %s", err)
		return nil, err
	}

	simulator := &ActionSimulator{
		kubeClient:  s.KubeClient,
		etcdStorage: etcdStorage,
	}

	if s.Action != "" {
		simulator.action = s.Action
	} else {
		glog.Warningf("--action was not specified.")
	}

	if s.Destination != "" {
		simulator.destination = s.Destination
	}

	if s.Pod != "" {
		simulator.pod = s.Pod
	} else {
		glog.Warningf("--pod was not specified.")

	}
	if s.Application != "" {
		simulator.application = s.Application
	}

	if s.VirtualApplication != "" {
		simulator.virtualApplication = s.VirtualApplication
	}

	if s.Namespace != "" {
		simulator.namespace = s.Namespace
	} else {
		glog.Warningf("--namespace was not specified. use default.")

		simulator.namespace = "default"
	}

	if s.Label != "" {
		simulator.label = s.Label
	}

	if s.NewReplica != "" {
		simulator.newReplica = s.NewReplica
	}

	return simulator, nil
}

// AddFlags adds flags for a specific SimulatorBuilder to the specified FlagSet
func (s *SimulatorBuilder) AddFlags(fs *pflag.FlagSet) *SimulatorBuilder {
	fs.IntVar(&s.Port, "port", s.Port, "The port that the scheduler's http service runs on")
	fs.StringVar(&s.Master, "master", s.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	fs.StringVar(&s.Kubeconfig, "kubeconfig", s.Kubeconfig, "Path to kubeconfig file with authorization and master location information.")
	fs.StringVar(&s.Namespace, "namespace", s.Namespace, "Namespace of the pod to be moved")
	fs.StringVar(&s.Pod, "pod", s.Pod, "Pod to be moved")
	fs.StringVar(&s.Application, "application", s.Application, "Applicaton to unbind")
	fs.StringVar(&s.VirtualApplication, "vapp", s.VirtualApplication, "VIrtual application to unbind")
	fs.StringVar(&s.Label, "label", s.Label, "The label of replication controller")
	fs.StringVar(&s.NewReplica, "replica", s.NewReplica, "New replica")
	fs.StringVar(&s.Action, "action", s.Action, "The action to take")
	fs.StringVar(&s.Destination, "destination", s.Destination, "IP of move destination. For move action exclusively")
	fs.StringSliceVar(&s.EtcdServerList, "etcd-servers", s.EtcdServerList, "List of etcd servers to watch (http://ip:port), comma separated. Mutually exclusive with -etcd-config")
	fs.StringVar(&s.EtcdCA, "cacert", s.EtcdCA, "Path to etcd ca.")
	fs.StringVar(&s.EtcdClientCertificate, "client-cert", s.EtcdClientCertificate, "Path to etcd client certificate")
	fs.StringVar(&s.EtcdClientKey, "client-key", s.EtcdClientKey, "Path to etcd client key")

	return s
}

// Run runs the specified SimulatorBuilder.  This should never exit.
func (s *SimulatorBuilder) Init(_ []string) *SimulatorBuilder {
	glog.V(3).Info("Try to get kubernetes client.")
	if s.Kubeconfig == "" && s.Master == "" {
		glog.Warningf("Neither --kubeconfig nor --master was specified.  Using default API client.  This might not work.")
	}

	if (s.EtcdConfigFile != "" && len(s.EtcdServerList) != 0) || (s.EtcdConfigFile == "" && len(s.EtcdServerList) == 0) {
		glog.Fatalf("specify either --etcd-servers or --etcd-config")
	}
	// This creates a client, first loading any specified kubeconfig
	// file, and then overriding the Master flag, if non-empty.
	kubeconfig, err := clientcmd.BuildConfigFromFlags(s.Master, s.Kubeconfig)
	if err != nil {
		glog.Errorf("Error getting kubeconfig:  %s", err)
		return nil
	}
	kubeconfig.QPS = 20.0
	kubeconfig.Burst = 30

	kubeClient, err := client.New(kubeconfig)
	if err != nil {
		glog.Fatalf("Invalid API configuration: %v", err)
	}
	s.KubeClient = kubeClient

	s.EtcdPathPrefix = DefaultEtcdPathPrefix

	return s
}

func newEtcd(client etcdclient.Client, pathPrefix string) (etcdStorage storage.Storage, err error) {

	simpleCodec := conversion.NewSimpleCodec()
	simpleCodec.AddKnownTypes(&registry.VMTEvent{})
	simpleCodec.AddKnownTypes(&registry.VMTEventList{})
	return etcdhelper.NewEtcdStorage(client, simpleCodec, pathPrefix), nil
}
