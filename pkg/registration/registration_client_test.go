package registration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

func xcheck(expected map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability,
	elements []*proto.ActionPolicyDTO_ActionPolicyElement,
) error {
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
	conf := NewRegistrationClientConfig(stitching.UUID, 0, true)
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

	expectedCapability := map[proto.EntityDTO_EntityType]map[proto.ActionItemDTO_ActionType]proto.ActionPolicyDTO_ActionCapability{
		pod: {
			move:      supported,
			resize:    notSupported,
			provision: supported,
			suspend:   supported,
		},
		container: {
			move:      notSupported,
			resize:    supported,
			provision: recommend,
			suspend:   recommend,
		},
		app: {
			move:      notSupported,
			resize:    recommend,
			provision: recommend,
			suspend:   recommend,
		},
		service: {
			move:      notSupported,
			resize:    notSupported,
			provision: notSupported,
			suspend:   notSupported,
		},
		node: {
			resize:    notSupported,
			provision: recommend,
			suspend:   recommend,
			scale:     notSupported,
		},
		wCtrl: {
			resize: supported,
			scale:  supported,
		},
		vol: {
			resize:    notSupported,
			provision: recommend,
			suspend:   notSupported,
			scale:     recommend,
		},
		bApp: {
			resize:    notSupported,
			provision: notSupported,
			suspend:   notSupported,
			scale:     notSupported,
		},
	}

	policies := reg.GetActionPolicy()

	for _, item := range policies {
		entity := item.GetEntityType()
		expected, ok := expectedCapability[entity]
		if !ok {
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
			assert.Equal(t, 2, len(policy.GetExecutionTargets()))
			assert.Equal(t, 4, len(resizeSpec.GetCommodityData()))
			commodities := resizeSpec.GetCommodityData()

			// Assert that the commodities contain the expected types
			commodityTypes := make(map[proto.CommodityDTO_CommodityType]bool)
			for _, commodity := range commodities {
				commodityTypes[commodity.GetCommodityType()] = true
			}
			assert.Contains(t, commodityTypes, proto.CommodityDTO_VCPU)
			assert.Contains(t, commodityTypes, proto.CommodityDTO_VMEM)
			assert.Contains(t, commodityTypes, proto.CommodityDTO_VCPU_REQUEST)
			assert.Contains(t, commodityTypes, proto.CommodityDTO_VMEM_REQUEST)
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
	conf := NewRegistrationClientConfig(stitching.UUID, 0, true)
	targetConf := &configs.K8sTargetConfig{}
	accountValues := []*proto.AccountValue{}
	k8sSvcId := "k8s-cluster"
	reg := NewK8sRegistrationClient(conf, targetConf, accountValues, k8sSvcId)

	// 1. all the entity types
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
		proto.EntityDTO_NODE_GROUP,
	}
	entitySet := make(map[proto.EntityDTO_EntityType]struct{})

	for _, etype := range entities {
		entitySet[etype] = struct{}{}
	}

	// 2. verify all the entity MetaData
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
