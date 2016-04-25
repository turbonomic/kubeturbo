package helper

import (
	"encoding/json"
	"io/ioutil"

	"github.com/golang/glog"
)

type TestingFlag struct {
	LocalTestingFlag     bool
	ActionTestingFlag    bool
	LocalTestStitchingIP string
}

func LoadTestingFlag(path string) (*TestingFlag, error) {
	file, err := ioutil.ReadFile(path)
	if err != nil {
		glog.V(3).Infof("File error: %v\n", err)
		return nil, err
	}
	var flags TestingFlag
	json.Unmarshal(file, &flags)
	glog.V(4).Infof("Results: %v\n", flags)
	return &flags, nil
}
