package dtofactory

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

func getTestEntityGroup(idCounter int) *repository.EntityGroup {
	eg := &repository.EntityGroup{
		Members:    make(map[metrics.DiscoveredEntityType][]string),
		ParentKind: fmt.Sprintf("Type-%d", idCounter),
		GroupId:    fmt.Sprintf("group1-%d", idCounter),
	}
	eg.Members[metrics.PodType] = []string{"app-pod-id1", "app-pod-id2", "app-pod-id3"}
	eg.Members[metrics.ContainerType] = []string{"app-container-id1", "app-container-id2", "app-container-id3",
		"istio-proxy-id1", "istio-proxy-id2", "istio-proxy-id3"}
	return eg
}

func TestSingleParentType(t *testing.T) {
	entityGroupMap := make(map[string]*repository.EntityGroup)
	targetId := "kube3"

	eg := getTestEntityGroup(1)
	entityGroupMap[eg.GroupId] = eg

	builder := &groupDTOBuilder{
		entityGroupMap: entityGroupMap,
		targetId:       targetId,
	}

	groupDTOs := builder.BuildGroupDTOs()

	// Expect 1 group for pods by parent and 1 group for containers by parent
	assert.True(t, len(groupDTOs) == 2)
	for _, groupDTO := range groupDTOs {
		assert.True(t, (groupDTO.GetEntityType() == proto.EntityDTO_CONTAINER_POD) || (groupDTO.GetEntityType() == proto.EntityDTO_CONTAINER))
	}
}

func TestMultipleParentTypesMultiplePods(t *testing.T) {
	entityGroupMap := make(map[string]*repository.EntityGroup)
	targetId := "kube3"

	for i := 0; i < 5; i++ {
		eg := getTestEntityGroup(i)
		entityGroupMap[eg.GroupId] = eg
	}
	builder := &groupDTOBuilder{
		entityGroupMap: entityGroupMap,
		targetId:       targetId,
	}

	groupDTOs := builder.BuildGroupDTOs()

	// Expect 1 group for pods by parent and 1 group for containers by parent
	// Total of 5 parents, total 10 groups
	assert.True(t, len(groupDTOs) == 10)
	for _, groupDTO := range groupDTOs {
		assert.True(t, (groupDTO.GetEntityType() == proto.EntityDTO_CONTAINER_POD) || (groupDTO.GetEntityType() == proto.EntityDTO_CONTAINER))
	}
}
