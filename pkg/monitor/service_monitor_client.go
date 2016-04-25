package monitor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"strings"

	"github.com/golang/glog"
)

// ServiceMonitorClient represents the base URL
type ServiceMonitorClient struct {
	baseUrl string
}

// NewServiceMonitorClient returns a new client with the specified base URL.
func NewServiceMonitorClient(url string) (*ServiceMonitorClient, error) {
	if !strings.HasSuffix(url, "/") {
		url += "/"
	}

	return &ServiceMonitorClient{
		baseUrl: url,
	}, nil
}

//
func (self *ServiceMonitorClient) TransactionInfo() (transactionInfo []Transaction, err error) {
	u := self.transactionInfoUrl()
	var ret []Transaction
	if err = self.httpGetJsonData(&ret, nil, u, "transaction info"); err != nil {
		glog.Errorf("Error getting Json Data for transactions: %s", err)
		return
	}
	transactionInfo = make([]Transaction, 0, len(ret))
	for _, cont := range ret {
		transactionInfo = append(transactionInfo, cont)
	}
	return
}

func (self *ServiceMonitorClient) transactionInfoUrl() string {
	return self.baseUrl + path.Join("transactions/")
}

func (self *ServiceMonitorClient) httpGetResponse(postData interface{}, url, infoName string) ([]byte, error) {
	var resp *http.Response
	var err error

	if postData != nil {
		data, err := json.Marshal(postData)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal data: %v", err)
		}
		resp, err = http.Post(url, "application/json", bytes.NewBuffer(data))
	} else {
		resp, err = http.Get(url)
	}
	if err != nil {
		return nil, fmt.Errorf("unable to get %q from %q: %v", infoName, url, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("received empty response for %q from %q", infoName, url)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		err = fmt.Errorf("unable to read all %q from %q: %v", infoName, url, err)
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("request %q failed with error: %q", url, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (self *ServiceMonitorClient) httpGetString(url, infoName string) (string, error) {
	body, err := self.httpGetResponse(nil, url, infoName)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (self *ServiceMonitorClient) httpGetJsonData(data, postData interface{}, url, infoName string) error {
	body, err := self.httpGetResponse(postData, url, infoName)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(body, data); err != nil {
		err = fmt.Errorf("unable to unmarshal %q (Body: %q) from %q with error: %v", infoName, string(body), url, err)
		return err
	}
	return nil
}
