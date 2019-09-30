package worker

import (
	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	k8sGroupWorkerID string = "GroupsDiscoveryWorker"
)

// Converts the cluster group objects to Group DTOs
type k8sEntityGroupDiscoveryWorker struct {
	id       string
	targetId string
	Cluster  *repository.ClusterSummary
}

func Newk8sEntityGroupDiscoveryWorker(cluster *repository.ClusterSummary,
	targetId string) *k8sEntityGroupDiscoveryWorker {
	return &k8sEntityGroupDiscoveryWorker{
		Cluster:  cluster,
		id:       k8sGroupWorkerID,
		targetId: targetId,
	}
}

// Group discovery worker collects groups discovered by different discovery workers.
// It merges the group members belonging to the same group but discovered by different workers.
// Then it creates DTOs for the groups to be sent to the server.
func (worker *k8sEntityGroupDiscoveryWorker) Do(entityGroupList []*repository.EntityGroup,
) ([]*proto.GroupDTO, error) {
	var groupDtos []*proto.GroupDTO

	// Entity groups per Owner type and instance
	entityGroupMap := make(map[string]*repository.EntityGroup)

	// Combine pod and container members discovered by different
	// discovery workers but belong to the same parent
	for _, entityGroup := range entityGroupList {
		groupId := entityGroup.GroupId
		existingGroup, groupExists := entityGroupMap[groupId]

		if groupExists {
			// groups by entity type
			for etype, newmembers := range entityGroup.Members {
				members, hasMembers := existingGroup.Members[etype]
				if hasMembers {
					existingGroup.Members[etype] = append(members, newmembers...)
				}
			}

			// groups by container names
			for containerName, newMembers := range entityGroup.ContainerGroups {
				members, hasMembers := existingGroup.ContainerGroups[containerName]
				if hasMembers {
					existingGroup.ContainerGroups[containerName] = append(members, newMembers...)
				}
			}

		} else {
			entityGroupMap[groupId] = entityGroup
		}
	}

	if glog.V(4) {
		for _, entityGroup := range entityGroupList {
			glog.Infof("Group --> %s::%s\n", entityGroup.ParentKind, entityGroup.ParentName)
			for etype, members := range entityGroup.Members {
				glog.Infof("	Members: %s -> %s", etype, members)
			}
			for cname, members := range entityGroup.ContainerGroups {
				glog.Infof("	Container Groups: %s -> %s", cname, members)
			}
		}
	}

	// Create DTOs for each group entity
	groupDtoBuilder, err := dtofactory.NewGroupDTOBuilder(entityGroupMap, worker.targetId)
	if err != nil {
		return groupDtos, err
	}

	groupDtos, _ = groupDtoBuilder.BuildGroupDTOs()

	// Create a static group for Master Nodes
	masterNodesGroupDTO := dtofactory.NewMasterNodesGroupDTOBuilder(worker.Cluster, worker.targetId).Build()
	if masterNodesGroupDTO != nil {
		groupDtos = append(groupDtos, masterNodesGroupDTO)
	}

	return groupDtos, nil
}
