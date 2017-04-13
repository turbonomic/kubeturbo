package kubelet

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/stats"
	kClient "k8s.io/kubernetes/pkg/kubelet/client"
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

// Execute the request
func (self *kubeletClient) postRequestAndGetValue(client *http.Client, req *http.Request, value interface{}) error {
	response, err := client.Do(req)
	if err != nil {
		return err
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

func (self *kubeletClient) GetPort() int {
	return int(self.config.Port)
}

func (self *kubeletClient) GetSummary(host Host) (*stats.Summary, error) {
	url := url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", host.IP, host.Port),
		Path:   "/stats/summary/",
	}
	if self.config != nil && self.config.EnableHttps {
		url.Scheme = "https"
	}

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return nil, err
	}
	summary := &stats.Summary{}
	client := self.client
	if client == nil {
		client = http.DefaultClient
	}
	err = self.postRequestAndGetValue(client, req, summary)
	return summary, err
}

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
