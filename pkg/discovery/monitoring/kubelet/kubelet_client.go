package kubelet

import (
	"fmt"
	"net/http"
	"net/url"

	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
	kClient "k8s.io/kubernetes/pkg/kubelet/client"

	"github.com/turbonomic/kubeturbo/pkg/discovery/util/httputil"

	cadvisorapi "github.com/google/cadvisor/info/v1"
)

const (
	summaryPath string = "/stats/summary/"
	specPath    string = "/spec"
)

type Host struct {
	IP       string
	Port     int
	Resource string
}

type kubeletClient struct {
	config *kClient.KubeletClientConfig
	client *http.Client
}

func (kc *kubeletClient) GetPort() int {
	return int(kc.config.Port)
}

func (kc *kubeletClient) GetSummary(host Host) (*stats.Summary, error) {
	requestURL := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", host.IP, host.Port),
		Path:   summaryPath,
	}
	if kc.config != nil && kc.config.EnableHttps {
		requestURL.Scheme = "https"
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

func (kc *kubeletClient) GetMachineInfo(host Host) (*cadvisorapi.MachineInfo, error) {
	requestURL := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", host.IP, host.Port),
		Path:   specPath,
	}
	if kc.config != nil && kc.config.EnableHttps {
		requestURL.Scheme = "https"
	}
	req, err := http.NewRequest("GET", requestURL.String(), nil)
	if err != nil {
		return nil, err
	}
	var minfo cadvisorapi.MachineInfo
	err = httputil.PostRequestAndGetValue(kc.client, req, &minfo)
	return &minfo, err
}

// Create a new Kubelet client from kubeletConfig.
func NewKubeletClient(kubeletConfig *kClient.KubeletClientConfig) (*kubeletClient, error) {
	transport, err := kClient.MakeTransport(kubeletConfig)
	if err != nil {
		return nil, err
	}
	c := &http.Client{
		Transport: transport,
		Timeout:   kubeletConfig.HTTPTimeout,
	}
	return &kubeletClient{
		config: kubeletConfig,
		client: c,
	}, nil
}
