package worker

import (
	"reflect"
	"testing"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
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
