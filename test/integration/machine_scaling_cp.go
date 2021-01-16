package integration

import (
	"github.com/turbonomic/kubeturbo/test/integration/framework"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	kubeclientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"

	. "github.com/onsi/ginkgo"
	"github.com/turbonomic/kubeturbo/pkg/action"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
)

var _ = Describe("Machine Scaling via Cloud Provider", func() {
	f := framework.NewTestFramework("machine-scaling-cp")
	var kubeConfig *restclient.Config
	var actionHandler *action.ActionHandler
	var kubeClient *kubeclientset.Clientset
	// Below are the examples how a nodepool(s) could be set via a command line arg
	//var nodeGroups = []string{"1:10:nodepool1", "1:10:nodepool2"}
	//var nodeGroups = []string{"3:10:agentpool"}
	var nodeGroups = []string{}
	var initialNodes = []string{}

	BeforeEach(func() {
		f.BeforeEach()
		// The following setup is shared across tests here
		if kubeConfig == nil {
			if framework.TestContext.NodeGroups != nil {
				nodeGroups = *framework.TestContext.NodeGroups
			}
			if len(nodeGroups) < 1 {
				framework.Failf("No nodegroups specified. Atleast one nodegroup should be specified for node Provision and suspend.")
			}

			kubeConfig := f.GetKubeConfig()
			kubeClient = f.GetKubeClient("machine-scaling-cp")
			dynamicClient, err := dynamic.NewForConfig(kubeConfig)
			if err != nil {
				framework.Failf("Failed to generate dynamic client for kubernetes test cluster: %v", err)
			}

			cluster.NewClusterScraper(kubeClient, dynamicClient)
			actionHandlerConfig := action.NewActionHandlerConfig("", nil, nil,
				cluster.NewClusterScraper(kubeClient, dynamicClient), nil, nil, true, "azure", nodeGroups)

			actionHandler = action.NewActionHandler(actionHandlerConfig)
		}
	})

	Describe("executing action provision machine", func() {
		It("should result in new node being added", func() {
			// get nodes
			// identify a random non master node
			// create a new action DTO with the target SE as this node
			// Execute the action
			// wait for new node
			initialNodes = f.GetClusterNodes()
			targetNode := initialNodes[0]
			_, err := actionHandler.ExecuteAction(newActionExecutionDTO(proto.ActionItemDTO_PROVISION,
				newSEFromNodeName(targetNode), nil), nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Provision failed for node: %s", targetNode)

			validateProvisionNode(f, kubeClient, initialNodes)

		})
	})

	Describe("executing action suspend machine", func() {
		It("should result in the node being deleted", func() {
			// get the newly provisioned node
			// create a new action DTO with the target SE as this node
			// Execute the action
			// wait for new node gone
			newNodes := f.GetClusterNodes()
			newProvisioned := getNewNode(initialNodes, newNodes)
			if newProvisioned == "" {
				framework.Failf("A New node hasn't been provisioned. Has a provision test run sequentially"+
					"before?. Old Nodes: %v, New Nodes: %v", initialNodes, newNodes)
			}
			_, err := actionHandler.ExecuteAction(newActionExecutionDTO(proto.ActionItemDTO_SUSPEND,
				newSEFromNodeName(newProvisioned), nil), nil, &mockProgressTrack{})
			framework.ExpectNoError(err, "Suspend failed for node: %s", newProvisioned)

			validateSuspendNode(f, kubeClient, newNodes)

		})
	})
})

func validateProvisionNode(f *framework.TestFramework, kubeClient *kubeclientset.Clientset, origNodes []string) {
	nodesNow := []string{}

	err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		nodesNow = f.GetClusterNodes()
		if len(nodesNow) > len(origNodes) {
			return true, nil
		}
		return false, nil
	})
	framework.ExpectNoError(err, "The PROVISION action failed. Original nodes: %v, New nodes: %v", origNodes, nodesNow)
}

func validateSuspendNode(f *framework.TestFramework, kubeClient *kubeclientset.Clientset, newNodes []string) {
	nodesNow := []string{}

	err := wait.PollImmediate(framework.PollInterval, framework.TestContext.SingleCallTimeout, func() (bool, error) {
		nodesNow = f.GetClusterNodes()
		if len(nodesNow) < len(newNodes) {
			return true, nil
		}
		return false, nil
	})
	framework.ExpectNoError(err, "The SUSPEND action failed. Original nodes: %v, New nodes: %v", newNodes, nodesNow)
}

func getNewNode(initial, new []string) string {
	for _, nNew := range new {
		match := false
		for _, nInitial := range initial {
			if nNew == nInitial {
				match = true
				break
			}
		}
		if !match {
			return nNew
		}
	}
	return ""
}
