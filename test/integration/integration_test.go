package integration

import (
	"os"
	"testing"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/test/integration/framework"

	"github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	"github.com/onsi/gomega"
)

// There are certain operations we only want to run once per overall test invocation
// (such as verifying that all system pods are running).
// Because of the way Ginkgo runs tests in parallel, we must use SynchronizedBeforeSuite
// to ensure that these operations only run on the first parallel Ginkgo node.
//
// This function takes two parameters: one function which runs on only the first Ginkgo node,
// returning an opaque byte array, and then a second function which runs on all Ginkgo nodes,
// accepting the byte array.
var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	// Run only on Ginkgo node 1

	// NOP for now
	// TODO
	return nil

}, func(data []byte) {
	// Run on all Ginkgo nodes
})

// Similar to SynchornizedBeforeSuite, we want to run some operations only once
// (such as collecting cluster logs, if needed at test completion).
// Here, the order of functions is reversed; first, the function which runs everywhere,
// and then the function that only runs on the first Ginkgo node.
var _ = ginkgo.SynchronizedAfterSuite(func() {
	// Run on all Ginkgo nodes
	// NOP for now
}, func() {
	// Run only Ginkgo on node 1
	// NOP for now
})

// RunIntegrationTests checks configuration parameters (specified through flags) and then runs
// tests using the Ginkgo runner.
// This function is called on each Ginkgo node in parallel mode.
func RunIntegrationTests(t *testing.T) {
	//gomega.RegisterFailHandler(ginkgowrapper.Fail)
	gomega.RegisterFailHandler(fail)
	glog.Infof("Starting integration run on Ginkgo node %d", config.GinkgoConfig.ParallelNode)
	ginkgo.RunSpecs(t, "Kubeturbo integration suite")
}

func fail(message string, callerSkip ...int) {
	glog.Info("Failed")
}

func TestMain(m *testing.M) {
	framework.ParseFlags()
	os.Exit(m.Run())
}

func TestIntegration(t *testing.T) {
	RunIntegrationTests(t)
}
