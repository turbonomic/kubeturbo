package framework

import (
	"flag"
	"fmt"
	"os"
	"time"

	"k8s.io/klog"
)

type TestContextType struct {
	KubeConfig        string
	KubeContext       string
	TestNamespace     string
	SingleCallTimeout time.Duration
}

var TestContext *TestContextType = &TestContextType{}

func registerFlags(t *TestContextType) {
	flag.StringVar(&t.KubeConfig, "kubeconfig", os.Getenv("KUBECONFIG"),
		"Path to kubeconfig containing embedded authinfo.")
	flag.StringVar(&t.KubeContext, "context", "",
		"kubeconfig context to use/override. If unset, will use value from 'current-context'.")
	flag.StringVar(&t.TestNamespace, "test-namespace", DefaultTestNS,
		fmt.Sprintf("The namespace that will be used as the seed name for tests.  If unset, will default to %q.", DefaultTestNS))
	flag.DurationVar(&t.SingleCallTimeout, "single-call-timeout", DefaultSingleCallTimeout,
		fmt.Sprintf("The maximum duration of a single call.  If unset, will default to %v", DefaultSingleCallTimeout))
}

func validateFlags(t *TestContextType) {
	if len(t.KubeConfig) == 0 {
		klog.Fatalf("kubeconfig is required")
	}
}

func ParseFlags() {
	registerFlags(TestContext)
	flag.Parse()
	validateFlags(TestContext)
}
