package processor

import (
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"k8s.io/api/core/v1"
	"strings"
)

// Class to query the multiple namespace objects data from the Kubernetes API server
// and create the resource quota for each
type ServiceProcessor struct {
	ClusterInfoScraper cluster.ClusterScraperInterface
	KubeCluster        *repository.KubeCluster
}

func NewServiceProcessor(kubeClient cluster.ClusterScraperInterface,
	kubeCluster *repository.KubeCluster) *ServiceProcessor {
	return &ServiceProcessor{
		ClusterInfoScraper: kubeClient,
		KubeCluster:        kubeCluster,
	}
}

// Query the Kubernetes API Server and Get the Service objects
func (p *ServiceProcessor) ProcessServices() {
	clusterName := p.KubeCluster.Name

	// The services
	serviceList, err := p.ClusterInfoScraper.GetAllServices()
	if err != nil {
		glog.Errorf("Failed to get services for cluster %s: %v.", clusterName, err)
		return
	}
	glog.V(2).Infof("There are %d services.", len(serviceList))

	// The service endpoint
	endpointList, err := p.ClusterInfoScraper.GetAllEndpoints()
	if err != nil {
		glog.Errorf("Failed to get service endpoints for cluster %s: %v.", clusterName, err)
		return
	}
	glog.V(2).Infof("There are %d service endpoints.", len(endpointList))

	// first make a endpoint map, key is endpoints cluster ID; value is endpoint object
	endpointMap := make(map[string]*v1.Endpoints)
	for _, endpoint := range endpointList {
		endpointClusterID := util.GetEndpointsClusterID(endpoint)
		endpointMap[endpointClusterID] = endpoint
	}

	// group Services and associated Pods
	svcPodMap := make(map[*v1.Service][]string)
	for _, service := range serviceList {
		serviceClusterID := util.GetServiceClusterID(service)
		serviceEndpoint, found := endpointMap[serviceClusterID]
		if !found || serviceEndpoint == nil {
			glog.Errorf("Cannot find endpoint for service %s", serviceClusterID)
			continue
		}
		podClusterIDs := findPodEndpoints(service, serviceEndpoint)
		if len(podClusterIDs) < 1 {
			glog.V(3).Infof("Service %s does not have any endpoint pod", serviceClusterID)
			continue
		}
		glog.V(3).Infof("Service %s with pod endpoints %v", serviceClusterID, podClusterIDs)

		svcPodMap[service] = podClusterIDs
	}

	for svc, podList := range svcPodMap {
		serviceClusterID := util.GetServiceClusterID(svc)
		glog.V(4).Infof("Service:%s --> %s\n", serviceClusterID, podList)
	}
	p.KubeCluster.Services = svcPodMap
}

// For every service, find the pods for this service.
func findPodEndpoints(service *v1.Service, serviceEndpoint *v1.Endpoints) []string {
	// find the endpoint for the service using the service cluster Id.
	// Endpoint associated with a service have the same cluster Id
	serviceClusterID := util.GetServiceClusterID(service)

	subsets := serviceEndpoint.Subsets
	podList := []string{}
	for _, endpointSubset := range subsets {
		addresses := endpointSubset.Addresses
		for _, address := range addresses {
			target := address.TargetRef
			if target == nil {
				continue
			}

			if !strings.EqualFold(target.Kind, "Pod") {
				// No need to log warning message if service endpoint is a node, as it is a valid case, for example:
				// kube-system/kubelet service
				if !strings.EqualFold(target.Kind, "Node") {
					glog.Warningf("service: %v depends on non-Pod entity with kind %v and name %v",
						serviceClusterID, target.Kind, target.Name)
				}
				continue
			}
			podName := target.Name
			podNamespace := target.Namespace
			// Pod Cluster Id
			podClusterID := util.BuildK8sEntityClusterID(podNamespace, podName)
			// get the pod name and the service name
			podList = append(podList, podClusterID)
		}
	}
	return podList
}
