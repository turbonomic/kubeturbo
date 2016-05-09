package helper

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/golang/glog"
)

var (
	// default flag path
	flagPath = "./pkg/helper/testing_flag.json"
)

type TestingFlag struct {
	LocalTestingFlag     bool
	ActionTestingFlag    bool
	LocalTestStitchingIP string
}

func SetPath(path string) {
	if _, err := os.Stat(path); err == nil {
		flagPath = path
		glog.V(2).Infof("VMT testing flag is load from file %s", path)
	} else {
		glog.Errorf("%s does not exist.", path)
	}
}

func LoadTestingFlag() (*TestingFlag, error) {
	file, err := ioutil.ReadFile(flagPath)
	if err != nil {
		glog.Errorf("File error: %v\n", err)
		return nil, err
	}
	var flags TestingFlag
	json.Unmarshal(file, &flags)
	glog.V(5).Infof("Results: %v\n", flags)
	return &flags, nil
}

func IsActionTesting() (bool, error) {
	flag, err := LoadTestingFlag()
	if err != nil {
		return false, err
	}
	return flag.ActionTestingFlag, nil
}
