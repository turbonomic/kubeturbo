package worker

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
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
func (worker *K8sControllerDiscoveryWorker) Do(cluster *repository.ClusterSummary, kubeControllers []*repository.KubeController) ([]*proto.EntityDTO, error) {
	// Map from controller UID to the corresponding kubeController
	kubeControllersMap := make(map[string]*repository.KubeController)
	// Combine the pods and allocation resources usage from different discovery workers but belonging to the same K8s controller.
	for _, kubeController := range kubeControllers {
		existingKubeController, exists := kubeControllersMap[kubeController.UID]
		if !exists {
			kubeControllersMap[kubeController.UID] = kubeController
		} else {
			for resourceType, resource := range existingKubeController.AllocationResources {
				usedValue, err := kubeController.GetResourceUsed(resourceType)
				if err != nil {
					glog.Error(err)
					continue
				}
				// Aggregate allocation resource usage of the same kubeController discovered by different discovery worker.
				// The usage values of CPU resources are all in millicores.
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
	workloadControllerDTOBuilder := dtofactory.NewWorkloadControllerDTOBuilder(cluster, kubeControllersMap, worker.cluster.NamespaceUIDMap)
	workloadControllerDtos, err := workloadControllerDTOBuilder.BuildDTOs()
	if err != nil {
		return nil, fmt.Errorf("error while creating WorkloadController entityDTOs: %v", err)
	}
	return workloadControllerDtos, nil
}
