package dtofactory

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
)

var (
	testCustomControllerType = "CustomControllerType"
	testClusterName          = "cluster"
	testNamespace            = "namespace"
	testNamespaceUID         = "namespace-UID"
	testResizable            = false

	testAllocationResources = []*repository.KubeDiscoveredResource{
		{
			Type:     metrics.CPULimitQuota,
			Capacity: 5327.0,
			Used:     5061.0,
		},
		{
			Type:     metrics.CPURequestQuota,
			Capacity: 2663.0,
			Used:     2397.0,
		},
		{
			Type:     metrics.MemoryLimitQuota,
			Capacity: 2097152.0,
			Used:     1945600.0,
		},
		{
			Type:     metrics.MemoryRequestQuota,
			Capacity: 1048576.0,
			Used:     921600.0,
		},
	}

	// Create testKubeController1 with allocation resources
	testKubeController1 = createKubeController(testClusterName, testNamespace, "controller1", util.KindDeployment,
		"controller1-UID", testAllocationResources)
	testKubeController2 = repository.NewKubeController(testClusterName, testNamespace, "controller2", testCustomControllerType, "controller2-UID")
	testKubeController3 = repository.NewKubeController(testClusterName, testNamespace, "controller3", util.KindCronJob, "controller3-UID")

	kubeCluster        = repository.KubeCluster{Name: clusterId}
	kubeClusterSummary = repository.ClusterSummary{KubeCluster: &kubeCluster}

	testWorkloadControllerDTOBuilder = NewWorkloadControllerDTOBuilder(&kubeClusterSummary,
		map[string]*repository.KubeController{
			"controller1-UID": testKubeController1,
			"controller2-UID": testKubeController2,
			"controller3-UID": testKubeController3,
		},
		map[string]string{
			testNamespace: testNamespaceUID,
		})
)

func TestBuildDTOs(t *testing.T) {
	// Mock the cluster summary data from which we retrieve the number of configured replicas on the
	// WorkloadController
	deploymentReplicaCount := int32(1)
	customControllerReplicaCount := int32(0)
	kubeCluster.ControllerMap = make(map[string]*repository.K8sController)
	for _, controller := range testWorkloadControllerDTOBuilder.kubeControllersMap {
		k8sController := repository.NewK8sController(
			"WorkloadController",
			controller.Name,
			controller.Namespace,
			controller.UID,
		)
		if controller.UID == testKubeController1.UID {
			k8sController.WithReplicas(int64(deploymentReplicaCount))
		} else if controller.UID == testKubeController2.UID {
			k8sController.WithReplicas(int64(customControllerReplicaCount))
		}
		kubeCluster.ControllerMap[controller.UID] = k8sController
	}

	entityDTOs, _ := testWorkloadControllerDTOBuilder.BuildDTOs()
	for _, entityDTO := range entityDTOs {
		if entityDTO.GetId() == "controller1-UID" {
			// cloneable and suspendable is false for WorkloadController
			actionEligibility := entityDTO.GetActionEligibility()
			assert.False(t, actionEligibility.GetCloneable())
			assert.False(t, actionEligibility.GetSuspendable())

			// Test commodity sold DTOs
			expectedCommoditiesSold := createCommoditiesSold(testKubeController1.UID)
			commoditiesSold := entityDTO.GetCommoditiesSold()
			assert.ElementsMatch(t, expectedCommoditiesSold, commoditiesSold)

			// Test commodity bought DTOs
			expectedCommoditiesBought := createCommoditiesBought(testNamespaceUID)
			commoditiesBought := entityDTO.GetCommoditiesBought()[0]
			assert.Equal(t, proto.EntityDTO_NAMESPACE, commoditiesBought.GetProviderType())
			assert.Equal(t, testNamespaceUID, commoditiesBought.GetProviderId())
			assert.ElementsMatch(t, expectedCommoditiesBought, commoditiesBought.GetBought())

			// Test create WorkloadControllerData with DeploymentData
			expectedWorkloadControllerData1 := &proto.EntityDTO_WorkloadControllerData{
				ControllerType: &proto.EntityDTO_WorkloadControllerData_DeploymentData{
					DeploymentData: &proto.EntityDTO_DeploymentData{},
				},
				ReplicaCount: &deploymentReplicaCount,
			}
			workloadControllerData1 := entityDTO.GetWorkloadControllerData()
			assert.EqualValues(t, expectedWorkloadControllerData1, workloadControllerData1)

			// Test Owns connection
			assert.Equal(t, 2, len(entityDTO.ConnectedEntities))
			assert.Equal(t, proto.ConnectedEntity_OWNS_CONNECTION, entityDTO.ConnectedEntities[1].GetConnectionType())
			assert.Equal(t, "controller1-UID/Foo", entityDTO.ConnectedEntities[1].GetConnectedEntityId())
		} else if entityDTO.GetId() == "controller2-UID" {
			// cloneable and suspendable is false for WorkloadController
			actionEligibility := entityDTO.GetActionEligibility()
			assert.False(t, actionEligibility.GetCloneable())
			assert.False(t, actionEligibility.GetSuspendable())

			// Test create WorkloadControllerData with CustomControllerData
			expectedWorkloadControllerData2 := &proto.EntityDTO_WorkloadControllerData{
				ControllerType: &proto.EntityDTO_WorkloadControllerData_CustomControllerData{
					CustomControllerData: &proto.EntityDTO_CustomControllerData{
						CustomControllerType: &testCustomControllerType,
					},
				},
				ReplicaCount: &customControllerReplicaCount,
			}
			workloadControllerData2 := entityDTO.GetWorkloadControllerData()
			assert.EqualValues(t, expectedWorkloadControllerData2, workloadControllerData2)
		}

		// Test AggregatedBy relationship
		assert.True(t, len(entityDTO.ConnectedEntities) > 0,
			fmt.Sprintf("WorkloadController %v should have at least one connected entity - the namespace",
				entityDTO.DisplayName))
		hasAggregatedBy := false
		for _, connectedEntity := range entityDTO.ConnectedEntities {
			if connectedEntity.GetConnectionType() == proto.ConnectedEntity_AGGREGATED_BY_CONNECTION {
				hasAggregatedBy = true
				assert.Equal(t, testNamespaceUID, connectedEntity.GetConnectedEntityId(),
					fmt.Sprintf("WorkloadController %v must be aggregated by namespace %v but is aggregated by %v instead",
						entityDTO.DisplayName, testNamespaceUID, connectedEntity.GetConnectedEntityId()))
			}
		}
		assert.True(t, hasAggregatedBy, fmt.Sprintf("Namespace %v does not have an AggregatedBy connection", entityDTO.DisplayName))

		// Test entity properties
		namespaceNameFromProperties, err := property.GetNamespaceFromProperties(entityDTO.GetEntityProperties())
		assert.Nil(t, err, "Error getting the namespace name from the entity properties")
		assert.Equal(t, testNamespace, namespaceNameFromProperties,
			"Namespace name from the entity properties does not match with expected")

		workloadControllerNameFromProperties, err := property.GetWorkloadControllerNameFromProperties(entityDTO.GetEntityProperties())
		assert.Nil(t, err, "Error getting the namespace name from the entity properties")
		assert.Equal(t, entityDTO.GetDisplayName(), workloadControllerNameFromProperties,
			"Workload controller name from the entity properties does not match with expected")

		fqnFromProperties, err := property.GetFullyQualifiedNameFromProperties(entityDTO.GetEntityProperties())
		assert.Nil(t, err, "Error getting the fully qualified name from the entity properties")
		assert.Equal(t, clusterId+util.NamingQualifierSeparator+testNamespace+util.NamingQualifierSeparator+entityDTO.GetDisplayName(), fqnFromProperties,
			"Workload controller fully qualified name from the entity properties does not match with expected")
	}
}

func TestGetContainerSpecs(t *testing.T) {
	kubeController := repository.NewKubeController("testCluster", "testNamespace", "test",
		util.KindDeployment, "controllerUID")
	pod1 := &api.Pod{
		Spec: api.PodSpec{
			Containers: []api.Container{
				{
					Name: "Foo",
				},
				{
					Name: "Bar",
				},
			},
		},
	}
	pod2 := &api.Pod{
		Spec: api.PodSpec{
			Containers: []api.Container{
				{
					Name: "Foo",
				},
				{
					Name: "Bar",
				},
			},
		},
	}
	kubeController.Pods = append(kubeController.Pods, pod1, pod2)
	containerSpecIds := testWorkloadControllerDTOBuilder.getContainerSpecIds(kubeController)
	expectedContainerSpecIds := []string{"controllerUID/Foo", "controllerUID/Bar"}
	assert.ElementsMatch(t, expectedContainerSpecIds, containerSpecIds)
}

func createKubeController(clustername, namespace, name, controllerType, uid string,
	testAllocationResources []*repository.KubeDiscoveredResource) *repository.KubeController {
	kubeController := repository.NewKubeController(clustername, namespace, name, controllerType, uid)
	for _, allocationResource := range testAllocationResources {
		kubeController.AddAllocationResource(allocationResource.Type, allocationResource.Capacity, allocationResource.Used)
	}
	pod := &api.Pod{
		Spec: api.PodSpec{
			Containers: []api.Container{
				{
					Name: "Foo",
				},
			},
		},
		Status: api.PodStatus{
			Phase: api.PodRunning,
		},
	}
	kubeController.Pods = append(kubeController.Pods, pod)
	return kubeController
}

func createCommoditiesSold(key string) []*proto.CommodityDTO {
	var commoditiesSold []*proto.CommodityDTO
	for _, resource := range testAllocationResources {
		commodityType, _ := rTypeMapping[resource.Type]
		commodityDTO := &proto.CommodityDTO{
			CommodityType: &commodityType,
			Key:           &key,
			Used:          &resource.Used,
			Peak:          &resource.Used,
			Capacity:      &resource.Capacity,
			Resizable:     &testResizable,
		}
		commoditiesSold = append(commoditiesSold, commodityDTO)
	}
	return commoditiesSold
}

func createCommoditiesBought(key string) []*proto.CommodityDTO {
	var commoditiesBought []*proto.CommodityDTO
	for _, resource := range testAllocationResources {
		commodityType, _ := rTypeMapping[resource.Type]
		commodityDTO := &proto.CommodityDTO{
			CommodityType: &commodityType,
			Key:           &key,
			Used:          &resource.Used,
			Peak:          &resource.Used,
		}
		commoditiesBought = append(commoditiesBought, commodityDTO)
	}
	return commoditiesBought
}

func TestCreateContainerPodDataByControllerType(t *testing.T) {
	// Test cases for supported controller kinds
	testCases := []struct {
		kind       string
		assertFunc func(*testing.T, *proto.EntityDTO_WorkloadControllerData)
	}{
		{
			kind: util.KindCronJob,
			assertFunc: func(t *testing.T, data *proto.EntityDTO_WorkloadControllerData) {
				assert.NotNil(t, data.GetCronJobData())
				assert.Nil(t, data.GetDaemonSetData())
			},
		},
		{
			kind: util.KindDaemonSet,
			assertFunc: func(t *testing.T, data *proto.EntityDTO_WorkloadControllerData) {
				assert.NotNil(t, data.GetDaemonSetData())
				assert.Nil(t, data.GetCronJobData())
			},
		},
		// Add test cases for other supported controller kinds
		{
			kind: util.KindDeployment,
			assertFunc: func(t *testing.T, data *proto.EntityDTO_WorkloadControllerData) {
				assert.NotNil(t, data.GetDeploymentData())
				// Add additional assertions if needed
			},
		},
		{
			kind: util.KindJob,
			assertFunc: func(t *testing.T, data *proto.EntityDTO_WorkloadControllerData) {
				assert.NotNil(t, data.GetJobData())
				// Add additional assertions if needed
			},
		},
		{
			kind: util.KindReplicaSet,
			assertFunc: func(t *testing.T, data *proto.EntityDTO_WorkloadControllerData) {
				assert.NotNil(t, data.GetReplicaSetData())
				// Add additional assertions if needed
			},
		},
		{
			kind: util.KindReplicationController,
			assertFunc: func(t *testing.T, data *proto.EntityDTO_WorkloadControllerData) {
				assert.NotNil(t, data.GetReplicationControllerData())
				// Add additional assertions if needed
			},
		},
		{
			kind: util.KindStatefulSet,
			assertFunc: func(t *testing.T, data *proto.EntityDTO_WorkloadControllerData) {
				assert.NotNil(t, data.GetStatefulSetData())
				// Add additional assertions if needed
			},
		},
	}

	// Run test cases
	for _, tc := range testCases {
		t.Run(tc.kind, func(t *testing.T) {
			data := CreateWorkloadControllerDataByControllerType(tc.kind)
			tc.assertFunc(t, data)
		})
	}

	// Test case for default (unsupported controller kind)
	t.Run("Default", func(t *testing.T) {
		kind := "UnsupportedKind"
		data := CreateWorkloadControllerDataByControllerType(kind)

		expectedData := &proto.EntityDTO_WorkloadControllerData{
			ControllerType: &proto.EntityDTO_WorkloadControllerData_CustomControllerData{
				CustomControllerData: &proto.EntityDTO_CustomControllerData{
					CustomControllerType: &kind,
				},
			},
		}

		if !reflect.DeepEqual(data, expectedData) {
			t.Errorf("Expected data = %v, but got %v", expectedData, data)
		}
	})
}
