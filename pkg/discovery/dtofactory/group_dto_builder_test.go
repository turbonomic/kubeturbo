package dtofactory

import (
	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"testing"
)

func testEntityGroupWithSingleContainerInPod() *repository.EntityGroup {
	eg := &repository.EntityGroup{
		Members:         make(map[metrics.DiscoveredEntityType][]string),
		ParentKind:      "Deployment",
		ParentName:      "app1",
		GroupId:         "group1",
		ContainerGroups: make(map[string][]string),
	}
	eg.Members[metrics.ContainerType] = []string{"app-container-id1", "app-container-id2", "app-container-id3"}
	eg.ContainerGroups["app-container"] = []string{"app-container-id1", "app-container-id2", "app-container-id3"}

	return eg
}

func testEntityGroupWithMultipleContainersInPod() *repository.EntityGroup {
	eg := &repository.EntityGroup{
		Members:         make(map[metrics.DiscoveredEntityType][]string),
		ParentKind:      "Deployment",
		ParentName:      "app1",
		GroupId:         "group1",
		ContainerGroups: make(map[string][]string),
	}
	eg.Members[metrics.ContainerType] = []string{"app-container-id1", "app-container-id2", "app-container-id3",
		"istio-proxy-id1", "istio-proxy-id2", "istio-proxy-id3"}
	eg.ContainerGroups["app-container"] = []string{"app-container-id1", "app-container-id2", "app-container-id3"}
	eg.ContainerGroups["istio-proxy"] = []string{"istio-proxy-id1", "istio-proxy-id2", "istio-proxy-id3"}

	return eg
}

func testStatefulSetWithMultipleContainersInPod() *repository.EntityGroup {
	eg := &repository.EntityGroup{
		Members:         make(map[metrics.DiscoveredEntityType][]string),
		ParentKind:      "StatefulSet",
		ParentName:      "ss-app1",
		GroupId:         "ss-group1",
		ContainerGroups: make(map[string][]string),
	}
	eg.Members[metrics.ContainerType] = []string{"app-container-id1", "app-container-id2", "app-container-id3",
		"istio-proxy-id1", "istio-proxy-id2", "istio-proxy-id3"}
	eg.ContainerGroups["app-container"] = []string{"app-container-id1", "app-container-id2", "app-container-id3"}
	eg.ContainerGroups["istio-proxy"] = []string{"istio-proxy-id1", "istio-proxy-id2", "istio-proxy-id3"}

	return eg
}

func testEntityGroupWithPod() *repository.EntityGroup {
	eg := &repository.EntityGroup{
		Members:         make(map[metrics.DiscoveredEntityType][]string),
		ParentKind:      "Deployment",
		ParentName:      "app1",
		GroupId:         "group1",
		ContainerGroups: make(map[string][]string),
	}
	eg.Members[metrics.PodType] = []string{"app-pod-id1", "app-pod-id2", "app-pod-id3"}
	eg.Members[metrics.ContainerType] = []string{"app-container-id1", "app-container-id2", "app-container-id3",
		"istio-proxy-id1", "istio-proxy-id2", "istio-proxy-id3"}

	eg.ContainerGroups["app-container"] = []string{"app-container-id1", "app-container-id2", "app-container-id3"}
	eg.ContainerGroups["istio-proxy"] = []string{"istio-proxy-id1", "istio-proxy-id2", "istio-proxy-id3"}

	return eg
}

func TestPodGroups(t *testing.T) {
	entityGroupMap := make(map[string]*repository.EntityGroup)
	targetId := "kube3"

	eg := testEntityGroupWithPod()
	entityGroupMap[eg.GroupId] = eg

	builder := &groupDTOBuilder{
		entityGroupMap: entityGroupMap,
		targetId:       targetId,
	}

	groupDTOs := builder.createGroupsByEntityType(eg, metrics.PodType, false)

	// Expect only parent group
	assert.True(t, len(groupDTOs) == 1)

	parentGroup := groupDTOs[0]
	resizeFlag := parentGroup.IsConsistentResizing
	assert.False(t, *resizeFlag)

	assert.True(t, parentGroup.GetEntityType() == proto.EntityDTO_CONTAINER_POD)
}

func TestContainerGroupsWithSingleContainerInPod(t *testing.T) {
	entityGroupMap := make(map[string]*repository.EntityGroup)
	targetId := "kube1"

	eg := testEntityGroupWithSingleContainerInPod()
	entityGroupMap[eg.GroupId] = eg

	builder := &groupDTOBuilder{
		entityGroupMap: entityGroupMap,
		targetId:       targetId,
	}

	groupDTOs := builder.createGroupsByEntityType(eg, metrics.ContainerType, true)

	// Expect only parent group
	assert.True(t, len(groupDTOs) == 1)

	parentGroup := groupDTOs[0]
	assert.True(t, parentGroup.GetEntityType() == proto.EntityDTO_CONTAINER)

	resizeFlag := parentGroup.IsConsistentResizing
	assert.True(t, *resizeFlag)
}

func TestContainerGroupsWithMultipleContainersInPod(t *testing.T) {
	entityGroupMap := make(map[string]*repository.EntityGroup)
	targetId := "kube2"

	eg := testEntityGroupWithMultipleContainersInPod()
	entityGroupMap[eg.GroupId] = eg

	builder := &groupDTOBuilder{
		entityGroupMap: entityGroupMap,
		targetId:       targetId,
	}

	groupDTOs := builder.createGroupsByEntityType(eg, metrics.ContainerType, true)

	// Expect parent and sub groups
	assert.True(t, len(groupDTOs) > 1)

	parentGroup := groupDTOs[0]
	assert.EqualValues(t, parentGroup.GetEntityType(), proto.EntityDTO_CONTAINER)

	resizeFlag := parentGroup.IsConsistentResizing
	assert.False(t, *resizeFlag)

	subGroups := groupDTOs[1:]
	for _, subgroup := range subGroups {
		assert.EqualValues(t, subgroup.GetEntityType(), proto.EntityDTO_CONTAINER)
		resizeFlag := subgroup.IsConsistentResizing
		assert.True(t, *resizeFlag)
	}
}

func TestStatefulSetGroupsWithMultipleContainersInPod(t *testing.T) {
	entityGroupMap := make(map[string]*repository.EntityGroup)
	targetId := "kube2"

	eg := testStatefulSetWithMultipleContainersInPod()
	entityGroupMap[eg.GroupId] = eg

	builder := &groupDTOBuilder{
		entityGroupMap: entityGroupMap,
		targetId:       targetId,
	}

	groupDTOs := builder.createGroupsByEntityType(eg, metrics.ContainerType, true)

	// Expect parent and sub groups
	assert.True(t, len(groupDTOs) > 1)

	parentGroup := groupDTOs[0]
	assert.EqualValues(t, parentGroup.GetEntityType(), proto.EntityDTO_CONTAINER)

	resizeFlag := parentGroup.IsConsistentResizing
	assert.False(t, *resizeFlag)

	subGroups := groupDTOs[1:]
	for _, subgroup := range subGroups {
		assert.EqualValues(t, subgroup.GetEntityType(), proto.EntityDTO_CONTAINER)
		resizeFlag := subgroup.IsConsistentResizing
		assert.True(t, *resizeFlag)
	}
}
