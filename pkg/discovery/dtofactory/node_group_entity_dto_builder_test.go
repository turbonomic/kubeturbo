package dtofactory

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/repository"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestNodeGroupEntityCreation(t *testing.T) {
	otherSpreadTopologyKeys := sets.NewString("zone", "region")
	table := []struct {
		nodes             []*api.Node
		expectedEntityNum int
	}{
		{
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"zone":   "zone1",
							"region": "region1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"zone":   "zone2",
							"region": "region1",
						},
					},
				},
			},
			expectedEntityNum: 3,
		},
		{
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"zone":   "zone1",
							"region": "region1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"zone":   "zone2",
							"region": "region1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node3",
						Labels: map[string]string{
							"zone":   "zone2",
							"region": "region2",
						},
					},
				},
			},
			expectedEntityNum: 4,
		},
		{
			nodes: []*api.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node1",
						Labels: map[string]string{
							"zone":                   "zone1",
							"region":                 "region1",
							"kubernetes.io/hostname": "node1",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node2",
						Labels: map[string]string{
							"zone":                   "zone2",
							"region":                 "region1",
							"kubernetes.io/hostname": "node2",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node3",
						Labels: map[string]string{
							"zone":                   "zone2",
							"region":                 "region2",
							"kubernetes.io/hostname": "node3",
						},
					},
				},
			},
			expectedEntityNum: 4,
		},
	}

	for _, item := range table {
		kubeCluster := repository.NewKubeCluster("MyCluster", item.nodes)
		clusterSummary := repository.CreateClusterSummary(kubeCluster)
		nodeGroupEntityDTOBuilder := NewNodeGroupEntityDTOBuilder(clusterSummary, nil, otherSpreadTopologyKeys)
		nodeGroupDTOs, _ := nodeGroupEntityDTOBuilder.BuildEntityDTOs()
		assert.Equal(t, item.expectedEntityNum, len(nodeGroupDTOs))
	}
}
