package k8sconntrack

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/turbonomic/kubeturbo/pkg/discovery/util/httputil"

	"github.com/golang/glog"
)

const (
	transactionPath string = "/transactions"
)

// A structure to specify host with K8sConntrack running.
type Host struct {
	IP   string
	Port int64
}

// Config for build K8sConntrack client.
type K8sConntrackClientConfig struct {
	schema string
	port   int64
}

// K8sConntrackClient is used to create RestAPI request to K8sConntrack agent and parse the response.
type K8sConntrackClient struct {
	config *K8sConntrackClientConfig
	client *http.Client
}

func NewK8sConntrackClient(config *K8sConntrackClientConfig) *K8sConntrackClient {
	return &K8sConntrackClient{
		config: config,
		client: http.DefaultClient,
	}
}

// Return port number of K8sConntrack.
func (c *K8sConntrackClient) GetPort() int64 {
	return c.config.port
}

// Get transaction data from provided host.
func (c *K8sConntrackClient) GetTransactionData(host Host) (transactions []Transaction, err error) {
	requestURL := url.URL{
		Scheme: c.config.schema,
		Host:   fmt.Sprintf("%s:%d", host.IP, host.Port),
		Path:   transactionPath,
	}
	req, err := http.NewRequest("GET", requestURL.String(), nil)
	if err != nil {
		return nil, err
	}
	var transactionsList []Transaction
	if err = httputil.PostRequestAndGetValue(c.client, req, &transactionsList); err != nil {
		glog.Errorf("Error getting Json Data for transactions: %s", err)
		return
	}
	transactions = make([]Transaction, 0, len(transactionsList))
	for _, count := range transactionsList {
		glog.V(4).Infof("transaction is %++v", count)
		transactions = append(transactions, count)
	}
	return
}
