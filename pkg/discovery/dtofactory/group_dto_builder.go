package dtofactory

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"github.com/turbonomic/turbo-go-sdk/pkg/builder/group"
	"github.com/golang/glog"
)

type groupDTOBuilder struct {
	policyGroupMap map[string]*repository.PolicyGroup
	nodeNames        []string
}

func NewGroupDTOBuilder(policyGroupMap map[string]*repository.PolicyGroup) *groupDTOBuilder {
	return &groupDTOBuilder{
		policyGroupMap: policyGroupMap,
	}
}

// Build entityDTOs based on the given node list.
func (builder *groupDTOBuilder) BuildGroupDTOs() ([]*proto.GroupDTO, error) {
	var result []*proto.GroupDTO
	eType := proto.EntityDTO_CONTAINER_POD

	for _, policyGroup := range builder.policyGroupMap {
		id := policyGroup.GroupId
		groupBuilder3 := group.StaticGroup(id).OfType(eType).WithEntities(policyGroup.Members)
		groupDTO , err := groupBuilder3.Build()
		if err != nil {
			glog.Errorf("Error creating group dto  %s::%s", id, err)
			continue
		}
		result = append(result, groupDTO)

		glog.V(1).Infof("groupDTO  : %++v\n", groupDTO)
	}

	return result, nil
}

