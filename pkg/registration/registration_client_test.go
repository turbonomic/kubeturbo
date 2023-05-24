package registration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

func xcheck(expected map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability,
	elements []*proto.ActionPolicyDTO_ActionPolicyElement) error {

	if len(expected) != len(elements) {
		return fmt.Errorf("length not equal: %d Vs. %d", len(expected), len(elements))
	}

	for _, e := range elements {
		action := e.GetActionType()
		capability := e.GetActionCapability()
		p, exist := expected[action]
		if !exist {
			return fmt.Errorf("action type(%v) not exist.", action)
		}

		if p != capability {
			return fmt.Errorf("action(%v) policy mismatch %v Vs %v", action, capability, p)
		}
	}

	return nil
}

func TestK8sRegistrationClient_GetActionPolicy(t *testing.T) {
	conf := NewRegistrationClientConfig(stitching.UUID, 0, true, true)
	targetConf := &configs.K8sTargetConfig{}
	accountValues := []*proto.AccountValue{}
	k8sSvcId := "k8s-cluster"
	reg := NewK8sRegistrationClient(conf, targetConf, accountValues, k8sSvcId)

	supported := proto.ActionPolicyDTO_SUPPORTED
	recommend := proto.ActionPolicyDTO_NOT_EXECUTABLE
	notSupported := proto.ActionPolicyDTO_NOT_SUPPORTED

	node := proto.EntityDTO_VIRTUAL_MACHINE
	pod := proto.EntityDTO_CONTAINER_POD
	container := proto.EntityDTO_CONTAINER
	app := proto.EntityDTO_APPLICATION_COMPONENT
	service := proto.EntityDTO_SERVICE
	wCtrl := proto.EntityDTO_WORKLOAD_CONTROLLER
	vol := proto.EntityDTO_VIRTUAL_VOLUME
	bApp := proto.EntityDTO_BUSINESS_APPLICATION

	move := proto.ActionItemDTO_MOVE
	resize := proto.ActionItemDTO_RIGHT_SIZE
	provision := proto.ActionItemDTO_PROVISION
	suspend := proto.ActionItemDTO_SUSPEND
	scale := proto.ActionItemDTO_SCALE

	expected_pod := make(map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability)
	expected_pod[move] = supported
	expected_pod[resize] = notSupported
	expected_pod[provision] = supported
	expected_pod[suspend] = supported

	expected_container := make(map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability)
	expected_container[move] = notSupported
	expected_container[resize] = supported
	expected_container[provision] = recommend
	expected_container[suspend] = recommend

	expected_app := make(map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability)
	expected_app[move] = notSupported
	expected_app[resize] = recommend
	expected_app[provision] = recommend
	expected_app[suspend] = recommend

	expected_service := make(map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability)
	expected_service[move] = notSupported
	expected_service[resize] = notSupported
	expected_service[provision] = notSupported
	expected_service[suspend] = notSupported

	expected_node := make(map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability)
	expected_node[resize] = notSupported
	expected_node[provision] = supported
	expected_node[suspend] = supported
	expected_node[scale] = notSupported

	expected_wCtrl := make(map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability)
	expected_wCtrl[resize] = supported
	expected_wCtrl[scale] = supported

	expected_vol := make(map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability)
	expected_vol[resize] = notSupported
	expected_vol[provision] = recommend
	expected_vol[suspend] = notSupported
	expected_vol[scale] = recommend

	expected_bApp := make(map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability)
	expected_bApp[resize] = notSupported
	expected_bApp[provision] = notSupported
	expected_bApp[suspend] = notSupported
	expected_bApp[scale] = notSupported

	policies := reg.GetActionPolicy()

	for _, item := range policies {
		entity := item.GetEntityType()
		expected := expected_pod

		if entity == pod {
			expected = expected_pod
		} else if entity == container {
			expected = expected_container
		} else if entity == app {
			expected = expected_app
		} else if entity == node {
			expected = expected_node
		} else if entity == service {
			expected = expected_service
		} else if entity == wCtrl {
			expected = expected_wCtrl
		} else if entity == vol {
			expected = expected_vol
		} else if entity == bApp {
			expected = expected_bApp
		} else {
			t.Errorf("Unknown entity type: %v", entity)
			continue
		}

		if err := xcheck(expected, item.GetPolicyElement()); err != nil {
			t.Errorf("Failed action policy check for entity(%v) %v", entity, err)
		}
	}
}

func TestK8sRegistrationClient_GetActionMergePolicy(t *testing.T) {
	rClient := &K8sRegistrationClient{} // Create an instance of the K8sRegistrationClient

	// Call the GetActionMergePolicy method
	policies := rClient.GetActionMergePolicy()

	// Perform assertions to validate the expected policies
	assert.NotNil(t, policies)
	assert.Equal(t, 2, len(policies))

	hasResizePolicy := false
	hasHorizontalScalePolicy := false

	for _, policy := range policies {
		// Validate the policy type
		resizeSpec := policy.GetResizeSpec()
		sloSpec := policy.GetHorizontalScaleSpec()

		if resizeSpec != nil {
			// Check the Resize policy
			hasResizePolicy = true
			assert.Equal(t, proto.EntityDTO_CONTAINER, policy.GetEntityType())
			assert.Equal(t, 0, len(policy.GetEntityFilters()))
		}

		if sloSpec != nil {
			// Check the HorizontalScale policy
			hasHorizontalScalePolicy = true
			assert.Equal(t, proto.EntityDTO_CONTAINER_POD, policy.GetEntityType())
			assert.Equal(t, 2, len(policy.GetEntityFilters()))

			hasDaemonSetData := false
			hasReplicaSetData := false

			for _, entityFilter := range policy.GetEntityFilters() {
				// Perform assertions on each entity filter
				containerPodData := entityFilter.GetContainerPodData()
				assert.NotNil(t, containerPodData)

				controllerData := containerPodData.GetControllerData()
				assert.NotNil(t, controllerData)

				switch controllerData.GetControllerType().(type) {
				case *proto.EntityDTO_WorkloadControllerData_DaemonSetData:
					// The entity filter has DaemonSetData
					hasDaemonSetData = true
					daemonSetData := controllerData.GetDaemonSetData()
					assert.NotNil(t, daemonSetData)

				case *proto.EntityDTO_WorkloadControllerData_ReplicaSetData:
					// The entity filter has ReplicaSetData
					hasReplicaSetData = true
					replicaSetData := controllerData.GetReplicaSetData()
					assert.NotNil(t, replicaSetData)

				default:
					assert.Fail(t, "Unknown ControllerData type")
				}
			}

			assert.True(t, hasDaemonSetData)
			assert.True(t, hasReplicaSetData)

			commodityData := sloSpec.GetCommodityData()
			assert.Equal(t, 2, len(commodityData))

			commodityTypes := make(map[proto.CommodityDTO_CommodityType]bool)
			for _, commodity := range commodityData {
				commodityTypes[commodity.GetCommodityType()] = true
			}

			assert.Contains(t, commodityTypes, proto.CommodityDTO_RESPONSE_TIME)
			assert.Contains(t, commodityTypes, proto.CommodityDTO_TRANSACTION)
		}
	}

	// Assert that one policy is for Resize and the other is for HorizontalScale
	assert.True(t, hasResizePolicy)
	assert.True(t, hasHorizontalScalePolicy)
}

func TestK8sRegistrationClient_GetEntityMetadata(t *testing.T) {
	conf := NewRegistrationClientConfig(stitching.UUID, 0, true, true)
	targetConf := &configs.K8sTargetConfig{}
	accountValues := []*proto.AccountValue{}
	k8sSvcId := "k8s-cluster"
	reg := NewK8sRegistrationClient(conf, targetConf, accountValues, k8sSvcId)

	//1. all the entity types
	entities := []proto.EntityDTO_EntityType{
		proto.EntityDTO_NAMESPACE,
		proto.EntityDTO_WORKLOAD_CONTROLLER,
		proto.EntityDTO_VIRTUAL_MACHINE,
		proto.EntityDTO_CONTAINER_SPEC,
		proto.EntityDTO_CONTAINER_POD,
		proto.EntityDTO_CONTAINER,
		proto.EntityDTO_APPLICATION_COMPONENT,
		proto.EntityDTO_SERVICE,
		proto.EntityDTO_VIRTUAL_VOLUME,
		proto.EntityDTO_CONTAINER_PLATFORM_CLUSTER,
		proto.EntityDTO_BUSINESS_APPLICATION,
	}
	entitySet := make(map[proto.EntityDTO_EntityType]struct{})

	for _, etype := range entities {
		entitySet[etype] = struct{}{}
	}

	//2. verify all the entity MetaData
	metaData := reg.GetEntityMetadata()
	if len(metaData) != len(entitySet) {
		t.Errorf("EntityMetadata count dis-match: %d vs %d", len(metaData), len(entitySet))
	}

	for _, meta := range metaData {
		etype := meta.GetEntityType()
		if _, exist := entitySet[etype]; !exist {
			t.Errorf("Unexpected EntityType: %v", etype)
		}

		properties := meta.GetNonVolatileProperties()
		if len(properties) != 1 {
			t.Errorf("Number of NonVolatieProperties should be 1 Vs. %v", len(properties))
		}

		if properties[0].GetName() != propertyId {
			t.Errorf("Property name should be : %v Vs. %v", propertyId, properties[0].GetName())
		}
	}

}
