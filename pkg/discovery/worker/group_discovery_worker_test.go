package worker

import (
	"fmt"
	"reflect"
	"testing"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestBuildMirrorPodGroup(t *testing.T) {
	worker := &k8sEntityGroupDiscoveryWorker{
		id:       "test_id",
		targetId: "target_id",
		cluster:  &repository.ClusterSummary{},
	}

	displayName := "Mirror Pods target_id"
	entityType := proto.EntityDTO_CONTAINER_POD
	ids := []string{
		"id1",
		"id2",
	}
	groupName := "Mirror-Pods-target_id"

	tests := []struct {
		uids     []string
		expected []*proto.GroupDTO
	}{
		{
			uids:     []string{},
			expected: nil,
		},
		{
			uids: ids,
			expected: []*proto.GroupDTO{
				newGroup(entityType, &displayName, ids, groupName, proto.GroupDTO_REGULAR, false),
			},
		},
	}

	for _, test := range tests {
		groupDTOs := worker.buildMirrorPodGroup(test.uids)
		if !reflect.DeepEqual(test.expected, groupDTOs) {
			t.Errorf("%s Failed. wanted: %++v, got: %++v", t.Name(), test.expected, groupDTOs)
		}
	}

}

func newGroup(entType proto.EntityDTO_EntityType, name *string, list []string, groupName string, groupT proto.GroupDTO_GroupType, resizing bool) *proto.GroupDTO {
	return &proto.GroupDTO{
		EntityType:  &entType,
		DisplayName: name,
		Info: &proto.GroupDTO_GroupName{
			GroupName: groupName,
		},
		GroupType:            &groupT,
		IsConsistentResizing: &resizing,
		Members: &proto.GroupDTO_MemberList{
			MemberList: &proto.GroupDTO_MembersList{
				Member: list,
			},
		},
	}
}

func TestBuildTopologySpreadConstraintsPodsGroup(t *testing.T) {
	generateExpectedPolicy := func(displayName string, members sets.String, policyDisplayName string, policyName string) []*proto.GroupDTO {
		entityType := proto.EntityDTO_CONTAINER_POD
		actionMode := "DISABLED"
		groupType := proto.GroupDTO_REGULAR
		isConsistentResizing := false

		groupDTO := &proto.GroupDTO{
			EntityType:  &entityType,
			DisplayName: &displayName,
			Info: &proto.GroupDTO_SettingPolicy_{
				SettingPolicy: &proto.GroupDTO_SettingPolicy{
					DisplayName: &policyDisplayName,
					Name:        &policyName,
					Settings: []*proto.GroupDTO_Setting{
						{
							Type: proto.GroupDTO_Setting_MOVE_AUTOMATION_MODE.Enum(),
							SettingValueType: &proto.GroupDTO_Setting_StringSettingValueType_{
								StringSettingValueType: &proto.GroupDTO_Setting_StringSettingValueType{
									Value: &actionMode,
								},
							},
						},
					},
				},
			},
			GroupType:            &groupType,
			IsConsistentResizing: &isConsistentResizing,
			Members: &proto.GroupDTO_MemberList{
				MemberList: &proto.GroupDTO_MembersList{
					Member: members.List(),
				},
			},
		}

		return []*proto.GroupDTO{groupDTO}
	}

	worker := &k8sEntityGroupDiscoveryWorker{
		id:       "id",
		targetId: "targetId",
		cluster:  &repository.ClusterSummary{},
	}

	displayName := "TopologySpreadConstraint Pods"
	mockUids := sets.NewString("uid1")
	policyDisplayName := fmt.Sprintf("TopologySpreadConstraint Pods Move Disabled [%s]", worker.targetId)
	policyName := fmt.Sprintf("TopologySpreadContraint Pods-%s", worker.targetId)

	tests := []struct {
		uids          sets.String
		expectedGroup []*proto.GroupDTO
	}{
		{
			uids:          sets.NewString(),
			expectedGroup: []*proto.GroupDTO{},
		},
		{
			uids:          mockUids,
			expectedGroup: generateExpectedPolicy(displayName, mockUids, policyDisplayName, policyName),
		},
	}

	for _, test := range tests {
		groupDTO := worker.buildTopologySpreadConstraintsPodsGroup(test.uids)
		if !reflect.DeepEqual(test.expectedGroup, groupDTO) {
			t.Errorf("%s Failed. Expected %v. Actual: %v", t.Name(), test.expectedGroup, groupDTO)
		}
	}
}
