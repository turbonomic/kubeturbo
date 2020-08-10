package integration

import (
	"fmt"

	"github.com/turbonomic/kubeturbo/test/integration/framework"
	"k8s.io/client-go/dynamic"
	restclient "k8s.io/client-go/rest"

	"github.com/golang/glog"
	. "github.com/onsi/ginkgo"
	"github.com/turbonomic/kubeturbo/pkg/action"
)

var _ = Describe("Action Executor", func() {
	f := framework.NewTestFramework("action-executor")
	var kubeConfig *restclient.Config
	var namespace string
	var actionHandler *action.ActionHandler
	BeforeEach(func() {
		// The following setup is shared across tests here
		if kubeConfig == nil {

			kubeConfig := f.GetKubeConfig()
			glog.V(3).Infof("kubeConfig: %+v", kubeConfig)
			kubeClient := f.GetKubeClient("action-executor")
			dynamicClient, err := dynamic.NewForConfig(kubeConfig)
			if err != nil {
				glog.Fatalf("Failed to generate dynamic client for kubernetes test cluster: %v", err)
			}

			actionHandlerConfig := action.NewActionHandlerConfig("", nil, kubeClient,
				nil, dynamicClient, nil, nil)

			actionHandler = action.NewActionHandler(actionHandlerConfig)
		}
		namespace = f.TestNamespaceName()
	})

	Describe("executes action", func() {
		testCases := map[string]struct {
		}{
			"move pod":        {},
			"update pod spec": {},
		}

		for testName, tc := range testCases {
			It(fmt.Sprintf("should result in %s in namespace %s", testName, namespace), func() {

			})
		}
	})
})
