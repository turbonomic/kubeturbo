package executor

import (
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	cloudBuilder "k8s.io/autoscaler/cluster-autoscaler/cloudprovider/builder"
	"k8s.io/autoscaler/cluster-autoscaler/config"
)

// machineSetController executes a machineSet scaling action request.
type CloudProviderController struct {
	CloudProvider cloudprovider.CloudProvider
	request       *actionRequest // The action request
}

//
// ------------------------------------------------------------------------------------------------------------------
//

func newCloudProviderController() Controller {
	opts := config.AutoscalingOptions{}
	cp := cloudBuilder.NewCloudProvider(opts)

	return &CloudProviderController{
		CloudProvider: cp,
	}
}

func (cp *CloudProviderController) checkPreconditions() error {
	return nil
}

func (cp *CloudProviderController) executeAction() error {
	return nil
}

func (cp *CloudProviderController) checkSuccess() error {
	return nil
}
