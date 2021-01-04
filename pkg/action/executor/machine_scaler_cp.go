package executor

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/client-go/kubernetes"

	"github.com/turbonomic/kubeturbo/pkg/action/util"
)

// machineSetController executes a machineSet scaling action request.
type CloudProviderController struct {
	cp         cloudprovider.CloudProvider
	request    *actionRequest // The action request
	kubeClient *kubernetes.Clientset
	node       *corev1.Node
}

//
// ------------------------------------------------------------------------------------------------------------------
//

func newCloudProviderController(nodeName string, diff int32, actionType ActionType,
	kubeClient *kubernetes.Clientset, cp cloudprovider.CloudProvider) Controller {
	request := &actionRequest{
		machineName: nodeName,
		diff:        diff,
		actionType:  actionType,
	}

	return &CloudProviderController{
		cp:         cp,
		request:    request,
		kubeClient: kubeClient,
	}
}

func (c *CloudProviderController) checkPreconditions() error {
	node, err := util.GetNodebyName(c.kubeClient, c.request.machineName)
	if err != nil {
		return err
	}
	c.node = node
	return nil
}

func (c *CloudProviderController) executeAction() error {
	nodeGroup, err := c.cp.NodeGroupForNode(c.node)
	if err != nil {
		return err
	}

	if c.request.actionType == ProvisionAction {
		// TODO: check if this is needed to be run in a parallel go routine
		return nodeGroup.IncreaseSize(int(c.request.diff))
	} else if c.request.actionType == SuspendAction {
		var nodes []*corev1.Node
		nodes = append(nodes, c.node)
		return nodeGroup.DeleteNodes(nodes)
	}

	return fmt.Errorf("Unknown action type :%s", c.request.actionType)
}

func (c *CloudProviderController) checkSuccess() error {

	// NOOP as of now as the cloudprovider calls wait for actions
	return nil
}
