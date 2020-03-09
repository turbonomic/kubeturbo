package compliance

import (
	api "k8s.io/api/core/v1"

	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

// affinityProcessorConfig defines necessary configuration for build an affinity processor.
type affinityProcessorConfig struct {
	// define how affinityProcessor accesses Kubernetes cluster.
	k8sClusterScraper *cluster.ClusterScraper
}

func NewAffinityProcessorConfig(k8sClusterScraper *cluster.ClusterScraper) *affinityProcessorConfig {
	return &affinityProcessorConfig{
		k8sClusterScraper: k8sClusterScraper,
	}
}

// Affinity processor parses each affinity rule defined in pod and creates commodityDTOs for nodes and pods.
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

// TODO if there is an error, fail the whole discovery? currently, error is handled in place and won't affect other discovery results.
func (am *AffinityProcessor) ProcessAffinityRules(entityDTOs []*proto.EntityDTO) []*proto.EntityDTO {
	am.GroupEntityDTOs(entityDTOs)
	podsNodesMap := buildPodsNodesMap(am.nodes, am.pods)

	for _, pod := range am.pods {
		am.processAffinityPerPod(pod, podsNodesMap)
	}
	return am.GetAllEntityDTOs()
}

func (am *AffinityProcessor) processAffinityPerPod(pod *api.Pod, podsNodesMap map[*api.Pod]*api.Node) {
	affinity := pod.Spec.Affinity
	if affinity == nil {
		return
	}

	nodeSelectorTerms := getAllNodeSelectors(affinity)
	nodeAffinityAccessCommoditiesSold, nodeAffinityAccessCommoditiesBought, err := am.commManager.GetAccessCommoditiesForNodeAffinity(nodeSelectorTerms)
	if err != nil {
		glog.Errorf("Failed to build commodity: %s", err)
		return
	}

	podAffinityTerms := getAllPodAffinityTerms(affinity)
	podAffinityCommodityDTOsSold, podAffinityCommodityDTOsBought, err := am.commManager.GetAccessCommoditiesForPodAffinityAntiAffinity(podAffinityTerms, pod)
	if err != nil {
		glog.Errorf("Failed to build commodity for pod affinity: %s", err)
		return
	}

	hostNode := podsNodesMap[pod]
	for _, node := range am.nodes {
		if matchesNodeSelector(pod, node) && matchesNodeAffinity(pod, node) {
			am.addAffinityAccessCommodities(pod, node, hostNode, nodeAffinityAccessCommoditiesSold, nodeAffinityAccessCommoditiesBought)
		}
		if interPodAffinityMatches(pod, node, podsNodesMap) {
			am.addAffinityAccessCommodities(pod, node, hostNode, podAffinityCommodityDTOsSold, podAffinityCommodityDTOsBought)
		}
	}
}

func getAllNodeSelectors(affinity *api.Affinity) []api.NodeSelectorTerm {
	// TODO we only parse RequiredDuringSchedulingIgnoredDuringExecution for now.
	if affinity.NodeAffinity == nil || affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		return []api.NodeSelectorTerm{}
	}
	return affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
}

func getAllPodAffinityTerms(affinity *api.Affinity) []api.PodAffinityTerm {
	podAffinityTerms := []api.PodAffinityTerm{}
	// TODO we only parse RequiredDuringSchedulingIgnoredDuringExecution for now.
	if affinity.PodAffinity != nil && affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		podAffinityTerms = append(podAffinityTerms, affinity.PodAffinity.RequiredDuringSchedulingIgnoredDuringExecution...)
	}
	// TODO we only parse RequiredDuringSchedulingIgnoredDuringExecution for now.
	if affinity.PodAntiAffinity != nil && affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
		podAffinityTerms = append(podAffinityTerms, affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution...)
	}
	//glog.Infof("pod selector terms are %++v", podAffinityTerms)
	return podAffinityTerms
}

func (am *AffinityProcessor) addAffinityAccessCommodities(pod *api.Pod, node *api.Node, hostingNode *api.Node,
	affinityAccessCommoditiesSold, affinityAccessCommoditiesBought []*proto.CommodityDTO) {

	// add commodity sold by matching nodes.
	if affinityAccessCommoditiesSold != nil && len(affinityAccessCommoditiesSold) > 0 {
		am.addCommoditySoldByNode(node, affinityAccessCommoditiesSold)
	}

	// add commodity bought by pod.
	if affinityAccessCommoditiesBought != nil && len(affinityAccessCommoditiesBought) > 0 {
		if hostingNode != nil {
			// We always use hosting node as provider while adding pods bought commodities
			// unless the pod does not have a node assigned yet (which is unlikely).
			// We do this to also honor a situation where a given pod needs a commodity
			// but is not available from the node its currently on.
			// We add the commodity bought under the hosting node as provider (which it might not
			// be selling anymore) to indicate that the pod still needs this commodity.
			//
			// Taking an example: 2 pods say pod1 & pod2 in a cluster with 3 nodes node1, node2 & node3.
			// At a given time t,
			//   - pod1 is on node1 (because of affinity to node1).
			//   - pod2 is created with pod affinity to pod1.
			//   - pod2 will be placed (by k8s scheduler) on node1 and we will add an access commodity
			//     say with key access-xxx123 (just an example, its a hash in reality) bought by pod2
			//     under provider as node1 (via addCommoditySoldByNode() above).
			//   - We additionally add the same access commodity (with key access-xxx123) under sold
			//     commodities by node1.
			// At time t2 pod1 changes its affinity and now is affined to node2 and moves to node2.
			//   - K8s ignores this change for pod2 as this is a run time change.
			//   - We find that pod2 has affinity to pod1 but not on the same node as pod1 and take
			//     following steps to ensure turbo generates an action to move pod2 also to node2:
			//     - We add an access commodity with key access-xxx123 bought by pod2 under provider
			//       as node1(pod1's current hostnode) (which is not intuitive. We do this because
			//       this pod is on node1 and is still buying all other commodities from node1 and
			//       this is how market understands the commodity need expressed for an entity).
			//     - Further this access commodity (with key access-xxx123) will now appear under
			//       sold commodities of node2 and not under node1 (via addCommoditySoldByNode() above).
			am.addCommodityBoughtByPod(pod, hostingNode, affinityAccessCommoditiesBought)
		} else {
			am.addCommodityBoughtByPod(pod, node, affinityAccessCommoditiesBought)
		}
	} // end if
}

func (am *AffinityProcessor) addCommoditySoldByNode(node *api.Node, affinityAccessCommodityDTOs []*proto.CommodityDTO) {
	nodeEntityDTO, err := am.GetEntityDTO(proto.EntityDTO_VIRTUAL_MACHINE, string(node.UID))
	if err != nil {
		glog.Errorf("Cannot find the entityDTO: %s", err)
		return
	}
	err = am.AddCommoditiesSold(nodeEntityDTO, affinityAccessCommodityDTOs...)
	if err != nil {
		glog.Errorf("Failed to add commodityDTO to %s: %s", node.Name, err)
	}
}

func (am *AffinityProcessor) addCommodityBoughtByPod(pod *api.Pod, node *api.Node, affinityAccessCommodityDTOs []*proto.CommodityDTO) {
	podEntityDTO, err := am.GetEntityDTO(proto.EntityDTO_CONTAINER_POD, string(pod.UID))
	if err != nil {
		glog.Errorf("Cannot find the entityDTO: %s", err)
		return
	}
	provider := sdkbuilder.CreateProvider(proto.EntityDTO_VIRTUAL_MACHINE, string(node.UID))
	err = am.AddCommoditiesBought(podEntityDTO, provider, affinityAccessCommodityDTOs...)
	if err != nil {
		glog.Errorf("Failed to add commodityDTOs to %s: %s", util.GetPodClusterID(pod), err)
	}
}

func buildPodsNodesMap(nodes []*api.Node, pods []*api.Pod) map[*api.Pod]*api.Node {
	nodesMap := make(map[string]*api.Node)
	for _, currNode := range nodes {
		nodesMap[currNode.Name] = currNode
	}
	podsNodesMap := make(map[*api.Pod]*api.Node)
	for _, currPod := range pods {
		hostingNode, exist := nodesMap[currPod.Spec.NodeName]
		if !exist || hostingNode == nil {
			continue
		}
		podsNodesMap[currPod] = hostingNode
	}
	return podsNodesMap
}
