package probe

import (
	"github.com/vmturbo/kubeturbo/pkg/helper"

	"github.com/golang/glog"
)

var localTestingFlag bool = false

var actionTestingFlag bool = false

var localTestStitchingIP string = ""

func init() {
	flag, err := helper.LoadTestingFlag()
	if err != nil {
		glog.Errorf("Error initialize probe package: %s", err)
		return
	}
	localTestingFlag = flag.LocalTestingFlag
	localTestStitchingIP = flag.LocalTestStitchingIP

	glog.V(4).Infof("Local %s", localTestingFlag)
	glog.V(4).Infof("Stitching IP %s", localTestStitchingIP)
}
