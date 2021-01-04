package executor

import (
	"fmt"
	"io"
	"os"

	"github.com/golang/glog"

	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider/azure"
	"k8s.io/autoscaler/cluster-autoscaler/config"
	"k8s.io/autoscaler/cluster-autoscaler/context"

	"github.com/turbonomic/kubeturbo/pkg/turbostore"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type MachineScalerType string

const (
	MachineScalerTypeCAPI MachineScalerType = "TypeCAPI" // Cluster API based scaler
	MachineScalerTypeCP   MachineScalerType = "TypeCP"   // Cloud provider based scaler
)

type MachineActionExecutor struct {
	executor      TurboK8sActionExecutor
	cache         *turbostore.Cache
	cAPINamespace string
	scalerType    MachineScalerType
	cp            cloudprovider.CloudProvider
}

func NewMachineActionExecutor(namespace string, ae TurboK8sActionExecutor, cp cloudprovider.CloudProvider) *MachineActionExecutor {
	return &MachineActionExecutor{
		executor:      ae,
		cache:         turbostore.NewCache(),
		cAPINamespace: namespace,
		cp:            cp,
	}
}

func (s *MachineActionExecutor) unlock(key string) {
	err := s.cache.Delete(key)
	if err != nil {
		glog.Errorf("Error unlocking action %v", err)
	}
}

// Execute : executes the scale action.
func (s *MachineActionExecutor) Execute(vmDTO *TurboActionExecutorInput) (*TurboActionExecutorOutput, error) {
	nodeName := vmDTO.ActionItems[0].GetTargetSE().GetDisplayName()
	var actionType ActionType
	var diff int32
	switch vmDTO.ActionItems[0].GetActionType() {
	case proto.ActionItemDTO_PROVISION:
		actionType = ProvisionAction
		diff = 1
		break
	case proto.ActionItemDTO_SUSPEND:
		actionType = SuspendAction
		diff = -1
		break
	default:
		return nil, fmt.Errorf("unsupported action type %v", vmDTO.ActionItems[0].GetActionType())
	}
	// Get on with it.
	var controller Controller
	var key string
	var err error
	if s.cp != nil {
		controller = newCloudProviderController(nodeName, diff, actionType, s.executor.clusterScraper.Clientset, s.cp)
		// For capi based scaler the key is the owner machineset on which the action happens.
		// For cp based scaler key is the nodename
		key = nodeName
	} else {
		controller, key, err = newCapiController(s.cAPINamespace, nodeName, diff, actionType,
			s.executor.cApiClient, s.executor.clusterScraper.Clientset)
		if err != nil {
			return nil, err
		}
	}

	if key == "" {
		return nil, fmt.Errorf("the target machine deployment has no name")
	}
	// See if we already have this.
	_, ok := s.cache.Get(key)
	if ok {
		return nil, fmt.Errorf("the action against the %s is already running", key)
	}
	s.cache.Add(key, key)
	defer s.unlock(key)
	// Check other preconditions.
	err = controller.checkPreconditions()
	if err != nil {
		return nil, err
	}
	err = controller.executeAction()
	if err != nil {
		return nil, err
	}
	err = controller.checkSuccess()
	if err != nil {
		return nil, err
	}
	return &TurboActionExecutorOutput{Succeeded: true}, nil
}

// CreateCloudProvider is necessary to be implemented here as the k8sautoscalers
// NewCloudProvder impl can panic on errors.
func CreateCloudProvider(providerName string) (cloudprovider.CloudProvider, error) {
	glog.V(1).Infof("Building %s cloud provider.", providerName)

	opts := config.AutoscalingOptions{}
	opts.CloudProviderName = providerName

	do := cloudprovider.NodeGroupDiscoveryOptions{
		NodeGroupSpecs:              opts.NodeGroups,
		NodeGroupAutoDiscoverySpecs: opts.NodeGroupAutoDiscovery,
	}

	rl := context.NewResourceLimiterFromAutoscalingOptions(opts)

	if opts.CloudProviderName == "" {
		glog.Warning("Returning a nil cloud provider")
		return nil, nil
	}

	switch opts.CloudProviderName {
	case cloudprovider.AzureProviderName:
		return BuildAzure(opts, do, rl)
	}

	glog.Warningf("Unknown cloud provider: %s", opts.CloudProviderName)
	return nil, nil
}

// BuildAzure builds Azure cloud provider, manager etc.
func BuildAzure(opts config.AutoscalingOptions, do cloudprovider.NodeGroupDiscoveryOptions, rl *cloudprovider.ResourceLimiter) (cloudprovider.CloudProvider, error) {
	var config io.ReadCloser
	if opts.CloudConfig != "" {
		glog.Infof("Creating Azure Manager using cloud-config file: %v", opts.CloudConfig)
		var err error
		config, err = os.Open(opts.CloudConfig)
		if err != nil {
			glog.Warningf("Couldn't open cloud provider configuration %s: %#v", opts.CloudConfig, err)
			return nil, err
		}
		defer config.Close()
	} else {
		glog.Info("Creating Azure Manager with default configuration.")
	}
	manager, err := azure.CreateAzureManager(config, do)
	if err != nil {
		glog.Warningf("Failed to create Azure Manager: %v", err)
		return nil, err
	}
	provider, err := azure.BuildAzureCloudProvider(manager, rl)
	if err != nil {
		glog.Warningf("Failed to create Azure cloud provider: %v", err)
		return nil, err
	}
	return provider, nil
}
