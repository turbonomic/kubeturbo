package dtofactory

import (
	"strings"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"k8s.io/apimachinery/pkg/util/sets"
)

type nodeGroupEntityDTOBuilder struct {
	clusterSummary *repository.ClusterSummary
}

const (
	affinityCommodityDefaultCapacity = 1e10
)

func NewNodeGroupEntityDTOBuilder(clusterSummary *repository.ClusterSummary) *nodeGroupEntityDTOBuilder {
	return &nodeGroupEntityDTOBuilder{
		clusterSummary: clusterSummary,
	}
}

// Build entityDTOs based on the given node list.
func (builder *nodeGroupEntityDTOBuilder) BuildEntityDTOs() ([]*proto.EntityDTO, error) {
	var result []*proto.EntityDTO
	nodeGrp2nodes := make(map[string]sets.String) // Map of nodeGroup ---> nodes
	for _, node := range builder.clusterSummary.Nodes {
		for key, value := range node.ObjectMeta.Labels {
			fullLabel := key + "=" + value
			nodeLst := nodeGrp2nodes[fullLabel]
			if nodeLst.Len() == 0 {
				nodeLst = sets.NewString(string(node.UID))
			} else {
				nodeLst.Insert(string(node.UID))
			}
			nodeGrp2nodes[fullLabel] = nodeLst
		}
	}
	// start building the NodeGroup entity dto
	for fullLabel, _ := range nodeGrp2nodes {
		labelparts := strings.Split(fullLabel, "=")
		if len(labelparts) > 0 && labelparts[0] == "kubernetes.io/hostname" {
			continue //no need to create entity for hostname label
		}
		entityDTOBuilder := sdkbuilder.NewEntityDTOBuilder(proto.EntityDTO_NODE_GROUP, fullLabel+"@"+builder.clusterSummary.Name)
		entityDto, err := entityDTOBuilder.Create()
		if err != nil {
			glog.Errorf("Failed to build NodeGroup entityDTO: %s", err)
			continue
		}

		result = append(result, entityDto)
		glog.V(4).Infof("NodeGroup DTO : %+v", entityDto)
	}
	return result, nil
}
