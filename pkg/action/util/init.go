package util

import (
	"github.com/turbonomic/kubeturbo/pkg/helper"

	"github.com/golang/glog"
)

var (
	localTestingFlag bool = false
)

func init() {
	flag, err := helper.LoadTestingFlag()
	if err != nil {
		glog.Errorf("Error initialize vmturbo package: %s", err)
		return
	}
	localTestingFlag = flag.LocalTestingFlag
}
