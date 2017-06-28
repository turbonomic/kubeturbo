package httputil

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

func PostRequestAndGetValue(client *http.Client, req *http.Request, value interface{}) error {
	response, err := client.Do(req)
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
