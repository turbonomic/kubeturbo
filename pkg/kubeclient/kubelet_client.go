package kubeclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/golang/glog"
	cadvisorapi "github.com/google/cadvisor/info/v1"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	v1 "k8s.io/api/core/v1"
	netutil "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
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
	ExecuteRequestAndGetValue(ip, nodeName, path string, value interface{}) error
	GetSummary(ip, nodeName string) (*stats.Summary, error)
	GetMachineInfo(ip, nodeName string) (*cadvisorapi.MachineInfo, error)
	GetNodeCpuFrequency(node *v1.Node) (float64, error)
}

// Cache structure.
// TODO(MB): Make sure the nodes that are no longer discovered are being removed from the cache!
type CacheEntry struct {
	statsSummary *stats.Summary
	// nodes cpu frequency in MHz as expected by server
	nodeCpuFreq *float64
	used        bool
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
		ip := repository.ParseNodeIP(node, v1.NodeInternalIP)
		if len(ip) == 0 {
			glog.Warningf("unable to obtain address for node %s, as it is no longer discovered", node.GetName())
		}
		names[ip] = true
	}
	// Cleanup
	count := 0
	for host, _ := range client.cache {
		_, ok := names[host]
		if !ok {
			glog.Warningf("removed host %s, as it is no longer discovered", host)
			delete(client.cache, host)
			count++
		}
	}
	return count
}

// Since http.Client is thread safe (https://golang.org/src/net/http/client.go)
// KubeletClient is also thread-safe if concurrent goroutines won't change the fields.
type KubeletClient struct {
	client              *http.Client
	scheme              string
	port                int
	cache               map[string]*CacheEntry
	cacheLock           sync.Mutex
	fallbkCpuFreqGetter *NodeCpuFrequencyGetter
	defaultCpuFreq      float64
	// Fallback kubernetes API client to fetch data from node's proxy subresource
	kubeClient         *kubernetes.Clientset
	forceProxyEndpoint bool
}

type statusNotFoundError struct {
	path string
}

func (s statusNotFoundError) Error() string {
	return fmt.Sprintf("%q was not found", s.path)
}

func (client *KubeletClient) ExecuteRequestAndGetValue(ip, nodeName, path string, value interface{}) error {
	var body []byte
	var err error
	if client.forceProxyEndpoint {
		body, err = client.callAPIServerProxyEndpoint(nodeName, path)
		if err != nil {
			return err
		}
	} else {
		body, err = client.callKubeletEndpoint(ip, path)
		if _, isStatusNotFoundError := err.(statusNotFoundError); isStatusNotFoundError {
			return err
		}
		if err != nil && client.kubeClient != nil {
			glog.V(2).Infof("The kubelet endpoint query for path %s to node: %s/%s did not work."+
				"Trying proxy endpoint.", path, nodeName, ip)
			body, err = client.callAPIServerProxyEndpoint(nodeName, path)
			if err != nil {
				return err
			}
		}
	}

	err = json.Unmarshal(body, value)
	if err != nil {
		return fmt.Errorf("failed to parse output. Response: %q. Error: %v", string(body), err)
	}
	return nil
}

func (client *KubeletClient) callKubeletEndpoint(ip, path string) ([]byte, error) {
	requestURL := url.URL{
		Scheme: client.scheme,
		Host:   fmt.Sprintf("%s:%d", ip, client.port),
		Path:   path,
	}

	req, err := http.NewRequest("GET", requestURL.String(), nil)
	if err != nil {
		return nil, err
	}

	httpClient := client.client
	response, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute the request: %s", err)
	}

	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body - %v", err)
	}
	if response.StatusCode == http.StatusNotFound {
		return nil, statusNotFoundError{path: req.URL.String()}
	} else if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed - %q, response: %q", response.Status, string(body))
	}

	return body, nil
}

func (client *KubeletClient) callAPIServerProxyEndpoint(nodeName, path string) ([]byte, error) {
	var statusCode int
	fullPath := fmt.Sprintf("%s%s%s%s", "/api/v1/nodes/", nodeName, "/proxy", path)
	body, err := client.kubeClient.CoreV1().RESTClient().Get().AbsPath(fullPath).Do(context.TODO()).StatusCode(&statusCode).Raw()
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusNotFound {
		return nil, statusNotFoundError{path: fullPath}
	} else if statusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed - %q, response: %q", statusCode, string(body))
	}

	return body, nil
}

func (client *KubeletClient) GetSummary(ip, nodeName string) (*stats.Summary, error) {
	// Get the data
	summary := &stats.Summary{}
	err := client.ExecuteRequestAndGetValue(ip, nodeName, summaryPath, summary)
	// Lock and check cache
	client.cacheLock.Lock()
	defer client.cacheLock.Unlock()
	entry, entryPresent := client.cache[ip]
	if err != nil {
		if entryPresent {
			entry.used = true
			if entry.statsSummary == nil {
				glog.V(2).Infof("unable to retrieve machine[%s/%s] summary: %v. The cached value unavailable", nodeName, ip, err)
				return nil, err
			}
			glog.V(2).Infof("unable to retrieve machine[%s/%s] summary: %v. Using cached value", nodeName, ip, err)
			// TODO(irfanurrehman): Improve the node check [fn checknode()].
			// This looks flawed. The same is also used as checknode;
			// if ExecuteRequestAndGetValue() returns error, checknode should get error
			// rather then a value from cache.
			return entry.statsSummary, nil
		} else {
			glog.Errorf("failed to get machine[%s/%s] summary: %v. No cache available", nodeName, ip, err)
			return summary, err
		}
	}
	// Fill in the cache
	if entryPresent {
		entry.used = false
		entry.statsSummary = summary
	} else {
		entry := &CacheEntry{
			statsSummary: summary,
			used:         false,
		}
		client.cache[ip] = entry
	}
	return summary, err
}

// get node single-core Frequency, in MHz
func (client *KubeletClient) GetNodeCpuFrequency(node *v1.Node) (float64, error) {
	ip, err := util.GetNodeIPForMonitor(node, types.KubeletSource)
	if err != nil {
		glog.Errorf("Failed to IP for node %s: %s", node.Name, err)
		return 0, err
	}

	// Get value from cache if exists
	client.cacheLock.Lock()
	entry, entryPresent := client.cache[ip]
	if entryPresent {
		if entry.nodeCpuFreq != nil {
			nodeFreq := *entry.nodeCpuFreq
			client.cacheLock.Unlock()
			return nodeFreq, nil
		}
	}
	client.cacheLock.Unlock()

	// We could not get a valid cpu frequency from cache, discover it.
	var nodeFreq float64
	minfo, err := client.GetMachineInfo(ip, node.Name)
	freqDiscovered := true
	if err != nil {
		glog.Errorf("Failed to get machine[%s] cpu.frequency from kubelet: %v. Will try pod based getter.", node.Name, err)
		nodeIsActive := util.NodeIsReady(node)
		if nodeIsActive {
			nodeFreq, err = client.fallbkCpuFreqGetter.GetFrequency(node.Name)
			if err != nil {
				glog.Errorf("Failed to get cpufreq from getter %s: %s. Default value will be used.", node.Name, err)
				freqDiscovered = false
			}
		} else {
			freqDiscovered = false
		}
	} else {
		nodeFreq = util.MetricKiloToMega(float64(minfo.CpuFrequency))
	}

	if !freqDiscovered {
		// We probably could not discover the cpufreq by any means and default
		// value is what we need to stick to. However, we might be able to discover
		// a valid one in the next discovery cycle.

		return client.defaultCpuFreq, nil
	}

	// We optimistically set a cpu frequency of last discovered node as default.
	// Clusters more often then not have similar nodes.
	client.defaultCpuFreq = nodeFreq

	// Update cache
	client.cacheLock.Lock()
	defer client.cacheLock.Unlock()
	if entryPresent {
		client.cache[ip].nodeCpuFreq = &nodeFreq
	} else {
		entry := &CacheEntry{
			statsSummary: nil,
			nodeCpuFreq:  &nodeFreq,
		}
		client.cache[ip] = entry
	}

	return nodeFreq, nil
}

func (client *KubeletClient) GetMachineInfo(ip, nodeName string) (*cadvisorapi.MachineInfo, error) {
	var minfo cadvisorapi.MachineInfo
	err := client.ExecuteRequestAndGetValue(ip, nodeName, specPath, &minfo)
	if err != nil {
		return nil, err
	}

	return &minfo, nil
}

func (client *KubeletClient) HasCacheBeenUsed(ip string) bool {
	client.cacheLock.Lock()
	defer client.cacheLock.Unlock()
	entry, entryPresent := client.cache[ip]
	if entryPresent {
		return entry.used
	}
	return false
}

// ----------------- kubeletConfig -----------------------------------
type KubeletConfig struct {
	kubeConfig           *rest.Config
	enableHttps          bool
	forceSelfSignedCerts bool
	port                 int
	timeout              time.Duration // timeout when fetching information from kubelet;
	tlsTimeOut           time.Duration
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

func (kc *KubeletConfig) ForceSelfSignedCerts(forceSelfSignedCerts bool) *KubeletConfig {
	kc.forceSelfSignedCerts = forceSelfSignedCerts
	return kc
}

func (kc *KubeletConfig) Timeout(timeout int) *KubeletConfig {
	kc.timeout = time.Duration(timeout) * time.Second
	return kc
}

func (kc *KubeletConfig) Create(fallbackClient *kubernetes.Clientset, busyboxImage string, useProxyEndpoint bool) (*KubeletClient, error) {
	// 1. http transport
	transport, err := makeTransport(kc.kubeConfig, kc.enableHttps, kc.tlsTimeOut, kc.forceSelfSignedCerts)
	if err != nil {
		return nil, err
	}
	c := &http.Client{
		Transport: transport,
		Timeout:   kc.timeout,
	}

	// 2. scheme
	scheme := "http"
	if kc.enableHttps {
		scheme = "https"
	}

	// 3. create a KubeletClient
	return &KubeletClient{
		client:              c,
		scheme:              scheme,
		port:                kc.port,
		cache:               make(map[string]*CacheEntry),
		fallbkCpuFreqGetter: NewNodeCpuFrequencyGetter(fallbackClient, busyboxImage),
		defaultCpuFreq:      DefaultCpuFreq,
		kubeClient:          fallbackClient,
		forceProxyEndpoint:  useProxyEndpoint,
	}, nil
}

// ------------Generate a http.Transport based on rest.Config-------------------
// Note: Following code is copied from Heapster
// https://github.com/kubernetes/heapster/blob/d2a1cf189921a68edd025d034ebdb348d7587509/metrics/sources/kubelet/util/kubelet_client.go#L48
// The reason to copy the code from Heapster, instead of using kubernetes/pkg/kubelet/client.MakeTransport(), is that
// Depending on Kubernetes will make it difficult to maintain the package dependency.
// So I copied this code, which only depending on "k8s.io/client-go".
func makeTransport(config *rest.Config, enableHttps bool, timeout time.Duration, forceSelfSignedCerts bool) (http.RoundTripper, error) {
	// 1. get transport.config
	cfg := transportConfig(config, enableHttps, forceSelfSignedCerts)
	tlsConfig, err := transport.TLSConfigFor(cfg)
	if err != nil {
		glog.Errorf("failed to get TLSConfig: %v", err)
		return nil, err
	}
	if tlsConfig == nil {
		glog.Warningf("tlsConfig is nil.")
	}

	// 2. http client
	rt := http.DefaultTransport
	if tlsConfig != nil {
		rt = netutil.SetOldTransportDefaults(&http.Transport{
			TLSClientConfig:     tlsConfig,
			TLSHandshakeTimeout: timeout,
		})
	}

	return transport.HTTPWrappersForConfig(cfg, rt)
}

func transportConfig(config *rest.Config, enableHttps bool, forceSelfSignedCerts bool) *transport.Config {
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
		glog.Warning("no valid certificate has been provided. Use self-signed certificates for the TLS transport.")
	} else if enableHttps && forceSelfSignedCerts {
		cfg.TLS.Insecure = true
		cfg.TLS.CAFile = ""
		cfg.TLS.CAData = []byte("")
		glog.Warning("self-signed certificate use for the TLS transport is enforced.")
	}

	return cfg
}
