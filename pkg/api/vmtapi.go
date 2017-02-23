package api

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/golang/glog"
)

type VmtApi struct {
	vmtUrl    string
	extConfig map[string]string
}

const (
	logger = "VMTurbo API"
)

func (vmtApi *VmtApi) AddK8sTarget(targetType, nameOrAddress, username, targetIdentifier, password string) error {
	glog.V(2).Infof("Calling VMTurbo REST API to added current %s target.", targetType)

	requestData := make(map[string]string)

	var requestDataBuffer bytes.Buffer

	requestData["type"] = targetType
	requestDataBuffer.WriteString("?type=")
	requestDataBuffer.WriteString(targetType)
	requestDataBuffer.WriteString("&")

	requestData["nameOrAddress"] = nameOrAddress
	requestDataBuffer.WriteString("nameOrAddress=")
	requestDataBuffer.WriteString(nameOrAddress)
	requestDataBuffer.WriteString("&")

	requestData["username"] = username
	requestDataBuffer.WriteString("username=")
	requestDataBuffer.WriteString(username)
	requestDataBuffer.WriteString("&")

	requestData["targetIdentifier"] = targetIdentifier
	requestDataBuffer.WriteString("targetIdentifier=")
	requestDataBuffer.WriteString(targetIdentifier)
	requestDataBuffer.WriteString("&")

	requestData["password"] = password
	requestDataBuffer.WriteString("password=")
	requestDataBuffer.WriteString(password)

	s := requestDataBuffer.String()

	respMsg, err := vmtApi.apiPost("/externaltargets", s)
	if err != nil {
		return err
	}
	glog.V(4).Infof("Add target response is %s", respMsg)

	return nil
}

// Discover a target using api
// http://localhost:8400/vmturbo/api/targets/k8s_vmt
func (vmtApi *VmtApi) DiscoverTarget(nameOrAddress string) error {
	glog.V(2).Info("Calling VMTurbo REST API to initiate a new discovery.")

	respMsg, err := vmtApi.apiPost("/targets/"+nameOrAddress, "")
	if err != nil {
		return err
	}
	glog.V(4).Infof("Discover target response is %s", respMsg)

	return nil
}

func (vmtApi *VmtApi) Post(postUrl, requestDataString string) (string, error) {
	return vmtApi.apiPost(postUrl, requestDataString)
}

func (vmtApi *VmtApi) Get(getUrl string) (string, error) {
	return vmtApi.apiGet(getUrl)
}

func (vmtApi *VmtApi) Delete(getUrl string) (string, error) {
	return vmtApi.apiDelete(getUrl)
}

// Call vmturbo api. return response
func (vmtApi *VmtApi) apiPost(postUrl, requestDataString string) (string, error) {
	fullUrl := vmtApi.vmtUrl + "/vmturbo/api" + postUrl + requestDataString
	glog.V(4).Info("The full Url is ", fullUrl)
	req, err := http.NewRequest("POST", fullUrl, nil)

	req.SetBasicAuth(vmtApi.extConfig["Username"], vmtApi.extConfig["Password"])
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	resp, err := client.Do(req)
	if err != nil {
		glog.Errorf("Error getting response: %s", err)
		return "", err
	}

	respContent, err := parseAPICallResponse(resp)
	if err != nil {
		glog.Errorf("Error getting response: %s", err)
		return "", err
	}
	glog.V(4).Infof("Post Succeed: %s", string(respContent))

	defer resp.Body.Close()
	return respContent, nil
}

// Call vmturbo api. return response
func (vmtApi *VmtApi) apiGet(getUrl string) (string, error) {
	fullUrl := vmtApi.vmtUrl + "/vmturbo/api" + getUrl
	glog.V(4).Infof("The full Url is %s", fullUrl)
	req, err := http.NewRequest("GET", fullUrl, nil)

	req.SetBasicAuth(vmtApi.extConfig["Username"], vmtApi.extConfig["Password"])
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}

	resp, err := client.Do(req)
	if err != nil {
		glog.Errorf("Error getting response: %s", err)
		return "", err
	}
	respContent, err := parseAPICallResponse(resp)
	if err != nil {
		glog.Errorf("Error getting response: %s", err)
		return "", err
	}
	glog.V(4).Infof("Get Succeed: %s", string(respContent))
	defer resp.Body.Close()
	return respContent, nil
}

// Delete API call
func (vmtApi *VmtApi) apiDelete(getUrl string) (string, error) {
	fullUrl := vmtApi.vmtUrl + "/vmturbo/api" + getUrl
	glog.V(4).Infof("The full Url is ", fullUrl)
	req, err := http.NewRequest("DELETE", fullUrl, nil)

	req.SetBasicAuth(vmtApi.extConfig["Username"], vmtApi.extConfig["Password"])
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	resp, err := client.Do(req)
	if err != nil {
		glog.Errorf("Error getting response: %s", err)
		return "", err
	}
	respContent, err := parseAPICallResponse(resp)
	if err != nil {
		glog.Errorf("Error getting response: %s", err)
		return "", err
	}
	glog.V(4).Infof("DELETE call Succeed: %s", string(respContent))
	defer resp.Body.Close()
	return respContent, nil
}

// this method takes in a reservation response and should return the reservation uuid, if there is any
func parseAPICallResponse(resp *http.Response) (string, error) {
	if resp == nil {
		return "", fmt.Errorf("response sent in is nil")
	}
	glog.V(4).Infof("response body is %s", resp.Body)

	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		glog.Errorf("Error after ioutil.ReadAll: %s", err)
		return "", err
	}
	glog.V(4).Infof("response content is %s", string(content))

	return string(content), nil
}

func NewVmtApi(url string, externalConfiguration map[string]string) *VmtApi {
	return &VmtApi{
		vmtUrl:    url,
		extConfig: externalConfiguration,
	}
}
