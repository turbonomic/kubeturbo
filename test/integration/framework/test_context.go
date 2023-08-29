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
	SingleCallTimeout time.Duration
	IsOpenShiftTest   bool
	DockerRegistry    string
	DockerUserName    string
	DockerUserPwd     string
	IsIstioEnabled    bool
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
	flag.BoolVar(&t.IsOpenShiftTest, "openshift-tests", false,
		"If set, the test will only run the Openshift case. By default, it's set to false, only the non-Openshift cases get run")
	flag.StringVar(&t.DockerRegistry, "docker-registry", "docker.io",
		"Docker registry. default is 'docker.io'")
	flag.StringVar(&t.DockerUserName, "docker-username", "",
		"The docker user name used to generate the pull secret.")
	flag.StringVar(&t.DockerUserPwd, "docker-password", "",
		"The docker user password used to generate the pull secret.")
	flag.BoolVar(&t.IsIstioEnabled, "istio-enabled", false,
		"If set, the namespace will be patched with label [istio-injection: enabled]. By default, it's set to false.")
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
