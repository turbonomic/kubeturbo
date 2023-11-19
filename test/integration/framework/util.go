package framework

import (
	"fmt"
	"time"

	"github.ibm.com/turbonomic/kubeturbo/test/integration/framework/ginkgowrapper"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	// Each API poll interval.
	PollInterval = 50 * time.Millisecond
	// How long to try/poll single API call.
	DefaultSingleCallTimeout = 120 * time.Second

	DefaultTestNS = "kubeturbo-test"
	BusyboxImage  = "icr.io/cpopen/turbonomic/busybox"
)

func nowStamp() string {
	return time.Now().Format(time.StampMilli)
}

func log(level string, format string, args ...interface{}) {
	fmt.Fprintf(GinkgoWriter, nowStamp()+": "+level+": "+format+"\n", args...)
}

func Errorf(format string, args ...interface{}) {
	log("ERROR", format, args...)
}

func Logf(format string, args ...interface{}) {
	log("INFO", format, args...)
}

func Failf(format string, args ...interface{}) {
	FailfWithOffset(1, format, args...)
}

// FailfWithOffset calls "Fail" and logs the error at "offset" levels above its caller
// (for example, for call chain f -> g -> FailfWithOffset(1, ...) error would be logged for "f").
func FailfWithOffset(offset int, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log("INFO", msg)
	ginkgowrapper.Fail(nowStamp()+": "+msg, 1+offset)
}

func Skipf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log("INFO", msg)
	ginkgowrapper.Skip(nowStamp() + ": " + msg)
}

func ExpectNoError(err error, explain ...interface{}) {
	ExpectNoErrorWithOffset(1, err, explain...)
}

// ExpectNoErrorWithOffset checks if "err" is set, and if so, fails assertion while logging the error at "offset" levels above its caller
// (for example, for call chain f -> g -> ExpectNoErrorWithOffset(1, ...) error would be logged for "f").
func ExpectNoErrorWithOffset(offset int, err error, explain ...interface{}) {
	if err != nil {
		Logf("Unexpected error occurred: %v", err)
	}
	ExpectWithOffset(1+offset, err).NotTo(HaveOccurred(), explain...)
}
