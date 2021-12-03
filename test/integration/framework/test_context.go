package framework

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/golang/glog"

	"k8s.io/klog"
)

type TestContextType struct {
	KubeConfig        string
	KubeContext       string
	TestNamespace     string
	ImagePullUserName string
	ImagePullPassword string
	SingleCallTimeout time.Duration
}

var TestContext *TestContextType = &TestContextType{}

func registerFlags(t *TestContextType) {
	flag.StringVar(&t.KubeConfig, "k8s-kubeconfig", os.Getenv("KUBECONFIG"),
		"Path to kubeconfig containing embedded authinfo.")
	flag.StringVar(&t.KubeContext, "k8s-context", "",
		"kubeconfig context to use/override. If unset, will use value from 'current-context'.")
	flag.StringVar(&t.TestNamespace, "test-namespace", DefaultTestNS,
		fmt.Sprintf("The namespace that will be used as the seed name for tests.  If unset, will default to %q.", DefaultTestNS))
	flag.StringVar(&t.ImagePullUserName, "image-pull-user-name", "",
		"The image registry user name that will be used to create the image pull secret.")
	flag.StringVar(&t.ImagePullPassword, "image-pull-password", "",
		"The image registry password that will be used to create the image pull secret.")
	flag.DurationVar(&t.SingleCallTimeout, "single-call-timeout", DefaultSingleCallTimeout,
		fmt.Sprintf("The maximum duration of a single call.  If unset, will default to %v", DefaultSingleCallTimeout))
}

func validateFlags(t *TestContextType) {
	if len(t.KubeConfig) == 0 {
		klog.Fatalf("kubeconfig is required")
	}
}

func ParseFlags() {
	initFlags(TestContext)
	validateFlags(TestContext)
}

func initFlags(t *TestContextType) {
	// These arguments can be overwritten from the command-line args
	_ = flag.Set("alsologtostderr", "true")
	registerFlags(t)
	flag.Parse()

	// Initialize klog specific flags into a new FlagSet
	klogFlags := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(klogFlags)

	// Add klog specific flags into goflag
	klogFlags.VisitAll(func(klogFlag *flag.Flag) {
		if flag.CommandLine.Lookup(klogFlag.Name) == nil {
			// This is a klog specific flag
			flag.CommandLine.Var(klogFlag.Value, klogFlag.Name, klogFlag.Usage)
		}
	})

	// Add log flush frequency
	flag.Duration("log-flush-frequency", 5*time.Second,
		"Maximum number of seconds between log flushes")

	// Sync the glog and klog flags
	flag.CommandLine.VisitAll(func(glogFlag *flag.Flag) {
		klogFlag := klogFlags.Lookup(glogFlag.Name)
		if klogFlag != nil {
			value := glogFlag.Value.String()
			_ = klogFlag.Value.Set(value)
		}
	})

	// Print out all parsed flags
	flag.VisitAll(func(flag *flag.Flag) {
		glog.V(2).Infof("FLAG: --%s=%q", flag.Name, flag.Value)
	})
}
