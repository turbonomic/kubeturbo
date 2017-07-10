package old

import (
	"fmt"

	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"

	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

var (
	// clusterID is a package level variable shared by different sub-probe.
	ClusterID string
)

type K8sProbe struct {
	stitchingManager  *stitching.StitchingManager
	k8sClusterScraper *cluster.ClusterScraper
	config            *configs.ProbeConfig
}

// Create a new Kubernetes probe with the given kubeClient.
func NewK8sProbe(k8sClusterScraper *cluster.ClusterScraper, config *configs.ProbeConfig) (*K8sProbe, error) {
	// First try to get cluster ID.
	if ClusterID == "" {
		id, err := k8sClusterScraper.GetKubernetesServiceID()
		if err != nil {
			return nil, fmt.Errorf("Error trying to get cluster ID:%s", err)
		}
		ClusterID = id
	}
	stitchingManager := stitching.NewStitchingManager(config.StitchingPropertyType)
	return &K8sProbe{
		stitchingManager:  stitchingManager,
		k8sClusterScraper: k8sClusterScraper,
		config:            config,
	}, nil
}

func (dc *K8sProbe) Discovery() ([]*proto.EntityDTO, error) {
	nodeEntityDtos, err := dc.ParseNode()
	if err != nil {
		// TODO make error dto
		return nil, fmt.Errorf("Error parsing nodes: %s. Will return.", err)
	}

	podEntityDtos, err := dc.ParsePod()
	if err != nil {
		glog.Errorf("Error parsing pods: %s. Skip.", err)
		// TODO make error dto
	}

	appEntityDtos, err := dc.ParseApplication()
	if err != nil {
		glog.Errorf("Error parsing applications: %s. Skip.", err)
	}

	serviceEntityDtos, err := dc.ParseService()
	if err != nil {
		// TODO, should here still send out msg to server? Or set errorDTO?
		glog.Errorf("Error parsing services: %s. Skip.", err)
	}

	entityDtos := nodeEntityDtos
	entityDtos = append(entityDtos, podEntityDtos...)
	entityDtos = append(entityDtos, appEntityDtos...)
	entityDtos = append(entityDtos, serviceEntityDtos...)

	return entityDtos, nil
}

func (k8sProbe *K8sProbe) ParseNode() ([]*proto.EntityDTO, error) {
	k8sNodes, err := k8sProbe.k8sClusterScraper.GetAllNodes()
	if err != nil {
		return nil, fmt.Errorf("Failed to get all nodes in Kubernetes cluster: %s", err)
	}

	k8sPods, err := k8sProbe.k8sClusterScraper.GetAllPods()
	if err != nil {
		return nil, fmt.Errorf("Failed to get all running pods in Kubernetes cluster: %s", err)
	}

	nodeProbe := NewNodeProbe(k8sProbe.config, k8sProbe.stitchingManager)
	return nodeProbe.parseNodeFromK8s(k8sNodes, k8sPods)
}

// Parse pods those are defined in namespace.
func (k8sProbe *K8sProbe) ParsePod() ([]*proto.EntityDTO, error) {
	k8sPods, err := k8sProbe.k8sClusterScraper.GetAllPods()
	if err != nil {
		return nil, fmt.Errorf("Failed to get all running pods in Kubernetes cluster: %s", err)
	}

	podProbe := NewPodProbe(k8sProbe.stitchingManager)
	return podProbe.parsePodFromK8s(k8sPods)
}

func (k8sProbe *K8sProbe) ParseApplication() ([]*proto.EntityDTO, error) {
	applicationProbe := NewApplicationProbe()
	return applicationProbe.ParseApplication()
}

func (k8sProbe *K8sProbe) ParseService() ([]*proto.EntityDTO, error) {
	serviceList, err := k8sProbe.k8sClusterScraper.GetAllServices()
	if err != nil {
		return nil, fmt.Errorf("Failed to get all services in Kubernetes cluster: %s", err)
	}
	endpointList, err := k8sProbe.k8sClusterScraper.GetAllEndpoints()
	if err != nil {
		return nil, fmt.Errorf("Failed to get all endpoints in Kubernetes cluster: %s", err)
	}

	svcProbe := NewServiceProbe()
	return svcProbe.ParseService(serviceList, endpointList)
}
