package flag

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/golang/glog"
	"errors"
)

var (
	flagPath = ""
)

type TestingFlag struct {
	LocalTestingFlag       bool
	LocalTestStitchingIP   string
}

func SetPath(path string) {
	if _, err := os.Stat(path); err == nil {
		flagPath = path
		glog.V(2).Infof("VMT testing flag is load from file %s", path)
	} else {
		glog.V(3).Infof("%s does not exist.", path)
	}
}

func LoadTestingFlag() (*TestingFlag, error) {
	if flagPath == "" {
		glog.V(4).Infof("Not a local testing.")
		return nil, errors.New("Not a local testing.")
	}
	file, err := ioutil.ReadFile(flagPath)
	if err != nil {
		glog.V(4).Infof("ERROR! : %v\n", err)
		return nil, err
	}
	var flags TestingFlag
	json.Unmarshal(file, &flags)
	glog.V(5).Infof("Results: %v\n", flags)
	return &flags, nil
}