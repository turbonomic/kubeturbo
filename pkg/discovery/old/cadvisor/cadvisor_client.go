// Copyright 2015 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Client library to programmatically access cAdvisor API.
package vmt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"strings"

	info "github.com/google/cadvisor/info/v2"

	"github.com/golang/glog"
)

// ClientV2 represents the base URL for a cAdvisor v2 client.
type ClientV2 struct {
	baseUrl string
}

// NewClient returns a new client with the specified base URL.
func NewV2Client(url string) (*ClientV2, error) {
	if !strings.HasSuffix(url, "/") {
		url += "/"
	}

	return &ClientV2{
		baseUrl: fmt.Sprintf("%sapi/v2.0/", url),
	}, nil
}

// MachineInfo returns the JSON machine information for this client.
// A non-nil error result indicates a problem with obtaining
// the JSON machine information data.
func (self *ClientV2) ProcessInfo() (psInfo []info.ProcessInfo, err error) {
	u := self.processInfoUrl()
	var ret []info.ProcessInfo
	if err = self.httpGetJsonData(&ret, nil, u, "process info"); err != nil {
		glog.Errorf("Error getting Json Data for process: %s", err)
		return
	}
	psInfo = make([]info.ProcessInfo, 0, len(ret))
	for _, cont := range ret {
		psInfo = append(psInfo, cont)
	}
	return
}

func (self *ClientV2) processInfoUrl() string {
	return self.baseUrl + path.Join("ps")
}

func (self *ClientV2) httpGetResponse(postData interface{}, url, infoName string) ([]byte, error) {
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

func (self *ClientV2) httpGetString(url, infoName string) (string, error) {
	body, err := self.httpGetResponse(nil, url, infoName)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func (self *ClientV2) httpGetJsonData(data, postData interface{}, url, infoName string) error {
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
