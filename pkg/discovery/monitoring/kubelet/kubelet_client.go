package kubelet

import (
	"fmt"
	"github.com/golang/glog"
	"net/http"
	"net/url"
	"time"

	cadvisorapi "github.com/google/cadvisor/info/v1"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util/httputil"
	netutil "k8s.io/apimachinery/pkg/util/net"
	api "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
	"strings"
)

const (
	summaryPath string = "/stats/summary/"
	specPath    string = "/spec"

	DefaultKubeletPort  = 10255
	DefaultKubeletHttps = false

	defaultConnTimeOut         = 20 * time.Second
	defaultTLSHandShakeTimeout = 10 * time.Second
)

// Since http.Client is thread safe (https://golang.org/src/net/http/client.go)
// KubeletClient is also thread-safe if concurrent goroutines won't change the fields.
type KubeletClient struct {
	client *http.Client
	scheme string
	port   int
}

func (kc *KubeletClient) ConnectToNodes(nodeList []*api.Node) error {
	var connectionErrors []error
	for _, node := range nodeList {
		var ip string
		for _, addr := range node.Status.Addresses {
			if addr.Type == api.NodeInternalIP && addr.Address != "" {
				ip = addr.Address
				break
			}
		}

		summary, err := kc.GetSummary(ip)
		glog.V(2).Infof("node:%s::%s ---> node name: %v\n", node.Name, ip, summary.Node.NodeName)
		if err != nil {
			connectionErrors = append(connectionErrors, fmt.Errorf("%s:%s", node.Name, err))
		}
	}

	if len(connectionErrors) == 0 {
		glog.V(2).Infof("Successfully connected to all nodes\n")
		return nil
	}
	var errorStr []string
	for i, err := range connectionErrors {
		errorStr = append(errorStr, fmt.Sprintf("Error %d: %s", i, err.Error()))
	}
	errStr := strings.Join(errorStr, "\n")
	glog.V(3).Infof("Errors connecting to nodes : %s\n", errStr)
	return fmt.Errorf(errStr)
}

func (kc *KubeletClient) GetSummary(host string) (*stats.Summary, error) {
	requestURL := url.URL{
		Scheme: kc.scheme,
		Host:   fmt.Sprintf("%s:%d", host, kc.port),
		Path:   summaryPath,
	}

	req, err := http.NewRequest("GET", requestURL.String(), nil)
	if err != nil {
		return nil, err
	}
	summary := &stats.Summary{}
	client := kc.client
	err = httputil.PostRequestAndGetValue(client, req, summary)
	return summary, err
}

func (kc *KubeletClient) GetMachineInfo(host string) (*cadvisorapi.MachineInfo, error) {
	requestURL := url.URL{
		Scheme: kc.scheme,
		Host:   fmt.Sprintf("%s:%d", host, kc.port),
		Path:   specPath,
	}
	req, err := http.NewRequest("GET", requestURL.String(), nil)
	if err != nil {
		return nil, err
	}
	var minfo cadvisorapi.MachineInfo
	err = httputil.PostRequestAndGetValue(kc.client, req, &minfo)
	return &minfo, err
}

// get machine single-core Frequency, in Khz
func (kc *KubeletClient) GetMachineCpuFrequency(host string) (uint64, error) {
	minfo, err := kc.GetMachineInfo(host)
	if err != nil {
		glog.Errorf("failed to get machine[%s] cpu.frequency: %v", host, err)
		return 0, err
	}

	return minfo.CpuFrequency, nil
}

//----------------- kubeletConfig -----------------------------------
type KubeletConfig struct {
	kubeConfig  *rest.Config
	enableHttps bool
	port        int
	timeout     time.Duration // timeout when fetching information from kubelet;
	tlsTimeOut  time.Duration
}

// Create a new KubeletConfig based on kubeConfig.
func NewKubeletConfig(kubeConfig *rest.Config) *KubeletConfig {
	return &KubeletConfig{
		kubeConfig:  kubeConfig,
		port:        DefaultKubeletPort,
		enableHttps: DefaultKubeletHttps,
		timeout:     defaultConnTimeOut,
		tlsTimeOut:  defaultTLSHandShakeTimeout,
	}
}

func (kc *KubeletConfig) WithPort(port int) *KubeletConfig {
	kc.port = port
	return kc
}

func (kc *KubeletConfig) EnableHttps(enable bool) *KubeletConfig {
	kc.enableHttps = enable
	return kc
}

func (kc *KubeletConfig) Timeout(timeout int) *KubeletConfig {
	kc.timeout = time.Duration(timeout) * time.Second
	return kc
}

func (kc *KubeletConfig) Create() (*KubeletClient, error) {
	//1. http transport
	transport, err := makeTransport(kc.kubeConfig, kc.enableHttps, kc.tlsTimeOut)
	if err != nil {
		return nil, err
	}
	c := &http.Client{
		Transport: transport,
		Timeout:   kc.timeout,
	}

	//2. scheme
	scheme := "http"
	if kc.enableHttps {
		scheme = "https"
	}

	//3. create a KubeletClient
	return &KubeletClient{
		client: c,
		scheme: scheme,
		port:   kc.port,
	}, nil
}

//------------Generate a http.Transport based on rest.Config-------------------
// Note: Following code is copied from Heapster
// https://github.com/kubernetes/heapster/blob/d2a1cf189921a68edd025d034ebdb348d7587509/metrics/sources/kubelet/util/kubelet_client.go#L48
// The reason to copy the code from Heapster, instead of using kubernetes/pkg/kubelet/client.MakeTransport(), is that
// Depending on Kubernetes will make it difficult to maintain the package dependency.
// So I copied this code, which only depending on "k8s.io/client-go".
func makeTransport(config *rest.Config, enableHttps bool, timeout time.Duration) (http.RoundTripper, error) {
	//1. get transport.config
	cfg := transportConfig(config, enableHttps)
	tlsConfig, err := transport.TLSConfigFor(cfg)
	if err != nil {
		glog.Errorf("failed to get TLSConfig: %v", err)
		return nil, err
	}
	if tlsConfig == nil {
		glog.Warningf("tlsConfig is nil.")
	}

	//2. http client
	rt := http.DefaultTransport
	if tlsConfig != nil {
		rt = netutil.SetOldTransportDefaults(&http.Transport{
			TLSClientConfig:     tlsConfig,
			TLSHandshakeTimeout: timeout,
		})
	}

	return transport.HTTPWrappersForConfig(cfg, rt)
}

func transportConfig(config *rest.Config, enableHttps bool) *transport.Config {
	cfg := &transport.Config{
		TLS: transport.TLSConfig{
			CAFile:   config.CAFile,
			CAData:   config.CAData,
			CertFile: config.CertFile,
			CertData: config.CertData,
			KeyFile:  config.KeyFile,
			KeyData:  config.KeyData,
		},
		BearerToken: config.BearerToken,
	}

	if enableHttps && !cfg.HasCA() {
		cfg.TLS.Insecure = true
		glog.Warning("insecure TLS transport.")
	}

	return cfg
}
