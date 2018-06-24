package kubeclient

import (
	"encoding/json"
	"fmt"
	"github.com/golang/glog"
	cadvisorapi "github.com/google/cadvisor/info/v1"
	"io/ioutil"
	netutil "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	summaryPath string = "/stats/summary/"
	specPath    string = "/spec"

	DefaultKubeletPort  = 10255
	DefaultKubeletHttps = false

	defaultConnTimeOut         = 20 * time.Second
	defaultTLSHandShakeTimeout = 10 * time.Second
)

type KubeHttpClientInterface interface {
	ExecuteRequestAndGetValue(host string, endpoint string, value interface{}) error
	GetSummary(host string) (*stats.Summary, error)
	GetMachineInfo(host string) (*cadvisorapi.MachineInfo, error)
	GetMachineCpuFrequency(host string) (uint64, error)
}

// Cache structure.
// TODO(MB): Make sure the nodes that are no longer discovered are being removed from the cache!
type CacheEntry struct {
	statsSummary *stats.Summary
	machineInfo  *cadvisorapi.MachineInfo
}

// Cleanup the cache.
// Returns number of deleted nodes
func (client *KubeletClient) CleanupCache(nodes []*v1.Node) int {
	// Lock and check cache
	client.cacheLock.Lock()
	defer client.cacheLock.Unlock()
	// Fill in the hash map for subsequent lookup
	names := make(map[string]bool)
	for _, node := range nodes {
		names[node.GetName()] = true
	}
	// Cleanup
	count := 0
	for host, _ := range client.cache {
		_, ok := names[host]
		if !ok {
			glog.Warningf("removed host %s, as it is no longer discovered")
			delete(client.cache, host)
			count++
		}
	}
	return count
}

// Since http.Client is thread safe (https://golang.org/src/net/http/client.go)
// KubeletClient is also thread-safe if concurrent goroutines won't change the fields.
type KubeletClient struct {
	client    *http.Client
	scheme    string
	port      int
	cache     map[string]*CacheEntry
	cacheLock sync.Mutex
}

func (client *KubeletClient) ExecuteRequestAndGetValue(host string, endpoint string, value interface{}) error {
	requestURL := url.URL{
		Scheme: client.scheme,
		Host:   fmt.Sprintf("%s:%d", host, client.port),
		Path:   endpoint,
	}

	req, err := http.NewRequest("GET", requestURL.String(), nil)
	if err != nil {
		return err
	}

	return client.postRequestAndGetValue(req, value)
}

func (client *KubeletClient) postRequestAndGetValue(req *http.Request, value interface{}) error {
	httpClient := client.client
	response, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute the request: %s", err)
	}
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body - %v", err)
	}
	if response.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%q was not found", req.URL.String())
	} else if response.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed - %q, response: %q", response.Status, string(body))
	}

	err = json.Unmarshal(body, value)
	if err != nil {
		return fmt.Errorf("failed to parse output. Response: %q. Error: %v", string(body), err)
	}
	return nil
}

func (client *KubeletClient) GetSummary(host string) (*stats.Summary, error) {
	// Get the data
	summary := &stats.Summary{}
	err := client.ExecuteRequestAndGetValue(host, summaryPath, summary)
	// Lock and check cache
	client.cacheLock.Lock()
	defer client.cacheLock.Unlock()
	entry, entryPresent := client.cache[host]
	if err != nil {
		if entryPresent {
			glog.V(2).Infof("unable to retrieve machine[%s] summary: %v. Using cached value", host, err)
			return entry.statsSummary, nil
		} else {
			glog.Errorf("failed to get machine[%s] summary: %v. No cache available", host, err)
			return summary, err
		}
	}
	// Fill in the cache
	if entryPresent {
		client.cache[host].statsSummary = summary
	} else {
		entry := &CacheEntry{
			statsSummary: summary,
			machineInfo:  nil,
		}
		client.cache[host] = entry
	}
	return summary, err
}

func (client *KubeletClient) GetMachineInfo(host string) (*cadvisorapi.MachineInfo, error) {
	// Get the data
	var minfo cadvisorapi.MachineInfo
	err := client.ExecuteRequestAndGetValue(host, specPath, &minfo)
	// Lock and check cache
	client.cacheLock.Lock()
	defer client.cacheLock.Unlock()
	entry, entryPresent := client.cache[host]
	if err != nil {
		if entryPresent {
			glog.V(2).Infof("unable to retrieve machine[%s] machine info: %v. Using cached value", host, err)
			return entry.machineInfo, nil
		} else {
			glog.Errorf("failed to get machine[%s] machine info: %v. No cache available", host, err)
			return &minfo, err
		}
	}
	// Fill in the cache
	if entryPresent {
		client.cache[host].machineInfo = &minfo
	} else {
		entry := &CacheEntry{
			statsSummary: nil,
			machineInfo:  &minfo,
		}
		client.cache[host] = entry
	}
	return &minfo, err
}

// get machine single-core Frequency, in Khz
func (client *KubeletClient) GetMachineCpuFrequency(host string) (uint64, error) {
	minfo, err := client.GetMachineInfo(host)
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
		cache:  make(map[string]*CacheEntry),
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
