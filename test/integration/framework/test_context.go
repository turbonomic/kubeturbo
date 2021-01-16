package framework

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang/glog"

	"k8s.io/klog"
)

// MultiStringFlag is a flag for passing multiple parameters using same flag
type MultiStringFlag []string

// String returns string representation of the node groups.
func (flag *MultiStringFlag) String() string {
	return "[" + strings.Join(*flag, " ") + "]"
}

// Set adds a new configuration.
func (flag *MultiStringFlag) Set(value string) error {
	*flag = strings.Split(value, ",")
	return nil
}

type TestContextType struct {
	KubeConfig        string
	KubeContext       string
	TestNamespace     string
	SingleCallTimeout time.Duration

	// NodeGroups is useful for the node provision and suspend via cloud provider tests
	NodeGroups *MultiStringFlag
}

var TestContext *TestContextType = &TestContextType{}

func registerFlags(t *TestContextType) {
	flag.StringVar(&t.KubeConfig, "k8s-kubeconfig", os.Getenv("KUBECONFIG"),
		"Path to kubeconfig containing embedded authinfo.")
	flag.StringVar(&t.KubeContext, "k8s-context", "",
		"kubeconfig context to use/override. If unset, will use value from 'current-context'.")
	flag.StringVar(&t.TestNamespace, "test-namespace", DefaultTestNS,
		fmt.Sprintf("The namespace that will be used as the seed name for tests.  If unset, will default to %q.", DefaultTestNS))
	flag.DurationVar(&t.SingleCallTimeout, "single-call-timeout", DefaultSingleCallTimeout,
		fmt.Sprintf("The maximum duration of a single call.  If unset, will default to %v", DefaultSingleCallTimeout))
	t.NodeGroups = new(MultiStringFlag)
	flag.Var(t.NodeGroups, "cp-node-groups", "The node group names when initialising the cloud provider with scale "+
		"min and max values. e.g. --cp-node-groups=1:10:nodegroup1,1:10:nodegroup2")
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
