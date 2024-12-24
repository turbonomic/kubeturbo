package flag

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"sync"

	"github.com/golang/glog"
)

var (
	flagPath = ""

	instance *TestingFlag
	once     sync.Once
)

type TestingFlag struct {
	LocalTestingFlag        bool
	LocalTestStitchingValue string
}

func GetFlag() *TestingFlag {
	once.Do(func() {
		instance = loadTestingFlag()
	})
	return instance
}

func loadTestingFlag() *TestingFlag {
	if flagPath == "" {
		glog.V(4).Infof("Not a local testing.")
		return nil
	}
	file, err := ioutil.ReadFile(flagPath)
	if err != nil {
		glog.V(4).Infof("ERROR! : %v\n", err)
		return nil
	}
	var flags TestingFlag
	err = json.Unmarshal(file, &flags)
	if err != nil {
		glog.Errorf("Failed to unmarshal file: %v", err)
	}
	return &flags
}

func SetPath(path string) {
	if _, err := os.Stat(path); err == nil {
		flagPath = path
		glog.V(2).Infof("VMT testing flag is load from file %s", path)
	} else {
		glog.V(3).Infof("%s does not exist.", path)
	}
}
