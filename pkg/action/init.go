package action

import (
	"github.com/vmturbo/kubeturbo/pkg/helper"

	"github.com/golang/glog"
)

var (
	localTestingFlag bool = false

	actionTestingFlag bool = false

	localTestStitchingIP string = ""
)

func init() {
	flag, err := helper.LoadTestingFlag()
	if err != nil {
		glog.Errorf("Error initialize vmturbo package: %s", err)
		return
	}
	localTestingFlag = flag.LocalTestingFlag
}
