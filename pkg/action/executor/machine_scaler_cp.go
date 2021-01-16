package executor

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/client-go/kubernetes"

	"github.com/golang/glog"
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
	actionType := c.request.actionType
	nodeName := c.node.Name
	if actionType != ProvisionAction && actionType != SuspendAction {
		return fmt.Errorf("unknown action type :%s", actionType)
	}

	nodeGroup, err := c.cp.NodeGroupForNode(c.node)
	if err != nil {
		return err
	}
	if nodeGroup == nil {
		return fmt.Errorf("action %s failed. Cloud provider nodegroup EMPTY found for node: %s.", actionType, nodeName)
	}

	// TargetSize() ensures that the poolsize if initialised to the current value
	// Without this the poolsize might remain unitialised in the DeleteNode() call.
	poolSize, err := nodeGroup.TargetSize()
	if err != nil {
		return err
	}
	glog.V(1).Infof("Executing action %s on node %s with node parent pool size %d.", actionType, nodeName, poolSize)
	if c.request.actionType == ProvisionAction {
		return nodeGroup.IncreaseSize(int(c.request.diff))
	} else if c.request.actionType == SuspendAction {
		var nodes []*corev1.Node
		nodes = append(nodes, c.node)
		return nodeGroup.DeleteNodes(nodes)
	}

	return nil
}

func (c *CloudProviderController) checkSuccess() error {
	// NOOP as of now because the cloudprovider calls wait for actions completion
	return nil
}
