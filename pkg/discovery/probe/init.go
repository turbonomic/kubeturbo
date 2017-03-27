package probe

import (
	"github.com/turbonomic/kubeturbo/test/flag"

	"github.com/golang/glog"
)

var localTestingFlag bool = false

var localTestStitchingIP string = ""

func init() {
	flag, err := flag.LoadTestingFlag()
	if err != nil {
		glog.Errorf("Error initialize probe package: %s", err)
		return
	}
	localTestingFlag = flag.LocalTestingFlag
	localTestStitchingIP = flag.LocalTestStitchingIP

	glog.V(4).Infof("Local %s", localTestingFlag)
	glog.V(4).Infof("Stitching IP %s", localTestStitchingIP)
}
