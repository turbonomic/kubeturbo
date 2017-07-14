package compliance

import (
	"k8s.io/apimachinery/pkg/labels"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

type affinityProcessorConfig struct {
	k8sClusterScraper *cluster.ClusterScraper
}

func NewAffinityProcessorConfig(k8sClusterScraper *cluster.ClusterScraper) *affinityProcessorConfig {
	return &affinityProcessorConfig{
		k8sClusterScraper: k8sClusterScraper,
	}
}

type AffinityProcessor struct {
	*ComplianceProcessor

	commManager *AffinityCommodityManager

	nodes []*api.Node
	pods  []*api.Pod
}

func NewAffinityProcessor(config *affinityProcessorConfig) (*AffinityProcessor, error) {
	allNodes, err := config.k8sClusterScraper.GetAllNodes()
	if err != nil {
		return nil, err
	}
	allPods, err := config.k8sClusterScraper.GetAllPods()
	if err != nil {
		return nil, err
	}
	return &AffinityProcessor{
		ComplianceProcessor: NewComplianceProcessor(),
		commManager:         NewAffinityCommodityManager(),

		nodes: allNodes,
		pods:  allPods,
	}, nil
}

// TODO if there is error, fail the whole discovery? currently, error is handle in place and won't affect other discovery results.
func (am *AffinityProcessor) ProcessNodeAffinity(entityDTOs []*proto.EntityDTO) []*proto.EntityDTO {
	am.GroupEntityDTOs(entityDTOs)
	for _, pod := range am.pods {
		am.processNodeAffinityPerPod(pod)
	}
	return am.GetAllEntityDTOs()
}

func (am *AffinityProcessor) processNodeAffinityPerPod(pod *api.Pod) {
	for _, node := range am.nodes {
		if matchesNodeSelector(pod, node) && matchesNodeAffinity(pod, node) {
			am.addAffinityAccessCommodity(pod, node)
		}
	}
}

func (am *AffinityProcessor) addAffinityAccessCommodity(pod *api.Pod, node *api.Node) {
	affinity := pod.Spec.Affinity
	if affinity != nil && affinity.NodeAffinity != nil {
		nodeAffinity := affinity.NodeAffinity
		glog.Infof("Add node affinity %s", nodeAffinity)

		if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			nodeSelectorTerms := nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			for _, term := range nodeSelectorTerms {
				affinityAccessCommodityDTOs, err := am.commManager.GetAccessCommoditiesForNodeAffinity(term.MatchExpressions)
				if err != nil {
					glog.Errorf("Failed to build commodity: %s", err)
					continue
				}

				// add commodity sold by matching nodes.
				nodeEntityDTO, err := am.GetEntityDTO(proto.EntityDTO_VIRTUAL_MACHINE, string(node.UID))
				if err != nil {
					glog.Errorf("Cannot find the entityDTO: %s", err)
					continue
				}
				err = am.AddCommoditiesSold(nodeEntityDTO, affinityAccessCommodityDTOs...)
				if err != nil {
					glog.Errorf("Failed to add commodityDTO to %s: %s", node.Name, err)
					continue
				} else {
					err = am.UpdateEntityDTO(nodeEntityDTO)
					if err != nil {
						glog.Errorf("Failed to update node entityDTO: %s", err)
						continue
					}
				}

				// add commodity bought by pod.
				if pod.Spec.NodeName == node.Name {
					podEntityDTO, err := am.GetEntityDTO(proto.EntityDTO_CONTAINER_POD, string(pod.UID))
					if err != nil {
						glog.Errorf("Cannot find the entityDTO: %s", err)
						continue
					}
					provider := sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, string(node.UID))
					err = am.AddCommoditiesBought(podEntityDTO, provider, affinityAccessCommodityDTOs...)
					if err != nil {
						glog.Errorf("Failed to add commodityDTOs to %s: %s", util.GetPodClusterID(pod), err)
					} else {
						err = am.UpdateEntityDTO(podEntityDTO)
						if err != nil {
							glog.Errorf("Failed to update pod entityDTO: %s", err)
							continue
						}
					}
				} // end if
			} // end for
		} // end if
	} // end if
}

func matchesNodeSelector(pod *api.Pod, node *api.Node) bool {
	// Check if node.Labels match pod.Spec.NodeSelector.
	if len(pod.Spec.NodeSelector) > 0 {
		selector := labels.SelectorFromSet(pod.Spec.NodeSelector)
		if !selector.Matches(labels.Set(node.Labels)) {
			return false
		}
	}
	return true
}

// The pod can only schedule onto nodes that satisfy requirements in both NodeAffinity and nodeSelector.
func matchesNodeAffinity(pod *api.Pod, node *api.Node) bool {
	nodeAffinityMatches := true
	affinity := pod.Spec.Affinity
	if affinity != nil && affinity.NodeAffinity != nil {
		nodeAffinity := affinity.NodeAffinity
		// if no required NodeAffinity requirements, will do no-op, means select all nodes.
		if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
			return true
		}

		// Match node selector for requiredDuringSchedulingIgnoredDuringExecution.
		if nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			nodeSelectorTerms := nodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
			glog.V(10).Infof("Match for RequiredDuringSchedulingIgnoredDuringExecution node selector terms %+v", nodeSelectorTerms)
			nodeAffinityMatches = nodeAffinityMatches && nodeMatchesNodeSelectorTerms(node, nodeSelectorTerms)
		}
	}
	return nodeAffinityMatches
}

// nodeMatchesNodeSelectorTerms checks if a node's labels satisfy a list of node selector terms,
// terms are ORed, and an empty list of terms will match nothing.
func nodeMatchesNodeSelectorTerms(node *api.Node, nodeSelectorTerms []api.NodeSelectorTerm) bool {
	for _, req := range nodeSelectorTerms {
		nodeSelector, err := NodeSelectorRequirementsAsSelector(req.MatchExpressions)
		if err != nil {
			glog.V(10).Infof("Failed to parse MatchExpressions: %+v, regarding as not match.", req.MatchExpressions)
			return false
		}
		if nodeSelector.Matches(labels.Set(node.Labels)) {
			return true
		}
	}
	return false
}
