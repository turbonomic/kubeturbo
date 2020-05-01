package worker

import (
	"fmt"
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// Converts the cluster K8s controller objects to entity DTOs
type K8sControllerDiscoveryWorker struct {
	cluster *repository.ClusterSummary
}

func NewK8sControllerDiscoveryWorker(cluster *repository.ClusterSummary) *K8sControllerDiscoveryWorker {
	return &K8sControllerDiscoveryWorker{
		cluster: cluster,
	}
}

// Controller discovery worker collects KubeController entities discovered by different discovery workers.
// It merges the pods belonging to the same controller but discovered by different discovery workers, and aggregates
// allocation resources usage of the same KubeController from different workers.
// Then it creates entity DTOs for the K8s controllers to be sent to the Turbonomic server.
// This runs after cluster NamespaceMap is populated.
func (worker *K8sControllerDiscoveryWorker) Do(kubeControllers []*repository.KubeController) ([]*proto.EntityDTO, error) {
	namespacesMap := worker.cluster.NamespaceMap
	// Map from controller UID to the corresponding kubeController
	kubeControllersMap := make(map[string]*repository.KubeController)
	// Combine the pods and allocation resources usage from different discovery workers but belonging to the same K8s controller.
	for _, kubeController := range kubeControllers {
		existingKubeController, exists := kubeControllersMap[kubeController.UID]
		if !exists {
			kubeNamespace, exists := namespacesMap[kubeController.Namespace]
			if !exists {
				glog.Errorf("Namespace %s does not exist in cluster %s", kubeController.Namespace, worker.cluster.Name)
				continue
			}
			averageNodeCpuFrequency := kubeNamespace.AverageNodeCpuFrequency
			if averageNodeCpuFrequency <= 0.0 {
				glog.Errorf("Average node CPU frequency is not larger than zero in namespace %s. Skip KubeController %s",
					kubeNamespace.Name, kubeController.GetFullName())
				continue
			}
			for resourceType, resource := range kubeController.AllocationResources {
				// For CPU resources, convert the capacity values expressed in number of cores to MHz.
				// Skip the conversion if capacity value is repository.DEFAULT_METRIC_CAPACITY_VALUE (infinity), which
				// means resource quota is not configured on the corresponding namespace.
				if metrics.IsCPUType(resourceType) && resource.Capacity != repository.DEFAULT_METRIC_CAPACITY_VALUE {
					newCapacity := resource.Capacity * kubeNamespace.AverageNodeCpuFrequency
					glog.V(4).Infof("Changing capacity of %s::%s from %f cores to %f MHz",
						kubeController.GetFullName(), resourceType, resource.Capacity, newCapacity)
					resource.Capacity = newCapacity
				}
			}
			kubeControllersMap[kubeController.UID] = kubeController
		} else {
			for resourceType, resource := range existingKubeController.AllocationResources {
				usedValue, err := kubeController.GetResourceUsed(resourceType)
				if err != nil {
					glog.Error(err)
					continue
				}
				// Aggregate allocation resource usage of the same kubeController discovered by different discovery worker.
				// The usage values of CPU resources have already been converted from cores to MHz based on the CPU
				// frequency of the corresponding node in controller_metrics_collector.
				resource.Used += usedValue
			}
			// Update the pod lists of the existing KubeController
			existingKubeController.Pods = append(existingKubeController.Pods, kubeController.Pods...)
		}

	}
	for _, kubeController := range kubeControllersMap {
		glog.V(4).Infof("Discovered WorkloadController entity: %s", kubeController)
	}
	// Create DTOs for each k8s WorkloadController entity
	workloadControllerDTOBuilder := dtofactory.NewWorkloadControllerDTOBuilder(kubeControllersMap, worker.cluster.NamespaceUIDMap)
	workloadControllerDtos, err := workloadControllerDTOBuilder.BuildDTOs()
	if err != nil {
		return nil, fmt.Errorf("error while creating WorkloadController entityDTOs: %v", err)
	}
	return workloadControllerDtos, nil
}
