package compliance

import (
	"github.com/golang/glog"
	api "k8s.io/api/core/v1"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// k8s marks a node as unschedulable when it sees a taint with below key by setting
// the field spec.unschedulable: true.
// we handle the unschedulable nodes by marking them with AvailableForPlacement = false,
// so we don't need taints processor to handle this taint via TAINT separately.
const unschedulableNodeTaintKey string = "node.kubernetes.io/unschedulable"

type NodeAndPodGetter interface {
	GetAllNodes() ([]*api.Node, error)
	GetAllPods() ([]*api.Pod, error)
}

// TaintTolerationProcessor parses taints defined in nodes and tolerations defined in pods and creates taint commodity
// DTOs sold by VMs and bought by Container Pods.
// See the detail in: https://vmturbo.atlassian.net/wiki/spaces/AE/pages/668598357/Taints+and+Tolerations+in+Kubernetes
type TaintTolerationProcessor struct {
	// Map of nodes indexed by node uid
	nodes map[string]*api.Node

	// Map of pods indexed by pod uid
	pods map[string]*api.Pod

	// Map of node uid indexed by node name
	nodeNameToUID map[string]string

	cluster *repository.ClusterSummary
}

func NewTaintTolerationProcessor(cluster *repository.ClusterSummary) (*TaintTolerationProcessor, error) {

	nodeMap := make(map[string]*api.Node)
	podMap := make(map[string]*api.Pod)
	nodeToPodsMap := cluster.NodeToRunningPods

	for nodeName, podList := range nodeToPodsMap {
		node := cluster.NodeMap[nodeName]
		if node == nil {
			continue
		}
		nodeMap[node.UID] = node.Node

		for _, pod := range podList {
			podMap[string(pod.UID)] = pod
		}
	}

	return &TaintTolerationProcessor{
		nodes:         nodeMap,
		pods:          podMap,
		nodeNameToUID: cluster.NodeNameUIDMap,
		cluster:       cluster,
	}, nil
}

// Process takes entityDTOs and add taint commodities for VMs and ContainerPods
// based on the taints and tolerations, respectively, in nodes and pods.
func (t *TaintTolerationProcessor) Process(entityDTOs []*proto.EntityDTO) {
	// Preprocess for node taints to create taint commodities for each node later.
	taintCollection := getTaintCollection(t.nodes)

	nodeDTOs, podDTOs := retrieveNodeAndPodDTOs(entityDTOs)

	t.createTaintCommoditiesSoldByNode(nodeDTOs, t.nodes, taintCollection)

	t.createTaintCommoditiesBoughtByPod(podDTOs, t.pods, t.nodeNameToUID, taintCollection)
}

// Creates taint commodities sold by VMs.
func (t *TaintTolerationProcessor) createTaintCommoditiesSoldByNode(nodeDTOs []*proto.EntityDTO,
	nodes map[string]*api.Node, taintCollection map[api.Taint]string) {
	for _, nodeDTO := range nodeDTOs {
		node, ok := nodes[*nodeDTO.Id]
		if !ok {
			glog.Errorf("Unable to find node object with uid %s::%s", *nodeDTO.Id, *nodeDTO.DisplayName)
			continue
		}
		// Taints
		taintComms, err := createTaintCommsSold(node, taintCollection)
		if err != nil {
			glog.Errorf("Error while processing taints for node %s", node.GetName())
			continue
		}
		nodeDTO.CommoditiesSold = append(nodeDTO.CommoditiesSold, taintComms...)
	}
}

// Creates taint commodities bought by ContainerPods.
func (t *TaintTolerationProcessor) createTaintCommoditiesBoughtByPod(podDTOs []*proto.EntityDTO, pods map[string]*api.Pod, nodeNameToUID map[string]string, taintCollection map[api.Taint]string) {
	for _, podDTO := range podDTOs {
		pod, ok := pods[*podDTO.Id]

		if !ok {
			glog.Errorf("Unable to find pod object with uid %s", *podDTO.DisplayName)
			continue
		}

		providerId, ok := nodeNameToUID[pod.Spec.NodeName]
		if !ok {
			glog.Errorf("Unable to find hosting node %s for pod %s/%s", pod.Spec.NodeName, pod.Namespace, pod.Name)
			continue
		}

		// Toleration
		taintComms, err := createTaintCommsBought(pod, taintCollection)
		if err != nil {
			glog.Errorf("Error while processing tolerations for pod %s/%s", pod.Namespace, pod.Name)
			continue
		}

		podBuysCommodities(podDTO, taintComms, providerId)
	}
}

// Appends taint commodities to the CommodityBought list in the ContainerPod DTO.
func podBuysCommodities(podDTO *proto.EntityDTO, comms []*proto.CommodityDTO, providerId string) {
	if len(comms) == 0 {
		return
	}

	for _, commBought := range podDTO.GetCommoditiesBought() {
		if commBought.GetProviderId() == providerId {
			glog.V(4).Infof("Found provider %s for pod %s to buy %d commodities", providerId, podDTO.GetDisplayName(), len(comms))
			commBought.Bought = append(commBought.GetBought(), comms...)
			return
		}
	}

	glog.Errorf("Unable to find commodity bought with provider %s for pod %s", providerId, *podDTO.DisplayName)
}

// Retrieves VM and ContainerPod DTOs from the DTO list.
func retrieveNodeAndPodDTOs(entityDTOs []*proto.EntityDTO) ([]*proto.EntityDTO, []*proto.EntityDTO) {
	var nodes []*proto.EntityDTO
	var pods []*proto.EntityDTO

	for _, dto := range entityDTOs {
		if dto.GetEntityType() == proto.EntityDTO_VIRTUAL_MACHINE {
			nodes = append(nodes, dto)
		} else if dto.GetEntityType() == proto.EntityDTO_CONTAINER_POD &&
			dto.GetPowerState() == proto.EntityDTO_POWERED_ON {
			pods = append(pods, dto)
		}
	}

	return nodes, pods
}

// Generates taint collection from taints in the node spec.
func getTaintCollection(nodes map[string]*api.Node) map[api.Taint]string {
	taintCollection := make(map[api.Taint]string)

	for _, node := range nodes {
		taints := node.Spec.Taints

		for _, taint := range taints {
			if !isUnschedulableNodeTaint(taint) && (taint.Effect == api.TaintEffectNoExecute ||
				taint.Effect == api.TaintEffectNoSchedule) {
				glog.V(2).Infof("Found taint %s on node %s)", taintCollection[taint], node.GetName())
				if _, found := taintCollection[taint]; !found {
					taintCollection[taint] = taint.Key + "=" + taint.Value + ":" + string(taint.Effect)
				}
			}
		}
	}

	glog.V(2).Infof("Created taint collection with %d taints found", len(taintCollection))

	return taintCollection
}

func isUnschedulableNodeTaint(taint api.Taint) bool {
	return taint.Key == unschedulableNodeTaintKey
}

// Creates taint commodities sold by VMs based on the taint collection.
func createTaintCommsSold(node *api.Node, taintCollection map[api.Taint]string) ([]*proto.CommodityDTO, error) {
	var taintComms []*proto.CommodityDTO

	taints := node.Spec.Taints
	nodeTaints := make(map[api.Taint]struct{})

	for _, taint := range taints {
		nodeTaints[taint] = struct{}{}
	}
	visited := make(map[string]bool, 0)
	for taint, key := range taintCollection {
		if visited[key] {
			glog.V(4).Infof("Commodity with key %s for taint %v already created for node %s", key, taint, node.Name)
			continue
		}
		// If the node doesn't contain the taint, create TAINT commodity
		if _, ok := nodeTaints[taint]; !ok {
			taintComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_TAINT).
				Key(key).
				Capacity(accessCommodityDefaultCapacity).
				Create()

			if err != nil {
				return nil, err
			}
			visited[key] = true
			glog.V(4).Infof("Created taint commodity with key %s for node %s", key, node.GetName())

			taintComms = append(taintComms, taintComm)
		}
	}

	glog.V(4).Infof("Created %d taint commodities for node %s", len(taintComms), node.GetName())
	return taintComms, nil
}

// Creates taint commodities bought by ContainerPods based on the taint collection and pod tolerations.
func createTaintCommsBought(pod *api.Pod, taintCollection map[api.Taint]string) ([]*proto.CommodityDTO, error) {
	var taintComms []*proto.CommodityDTO

	visited := make(map[string]bool, 0)
	for taint, key := range taintCollection {
		if visited[key] {
			glog.V(4).Infof("Commodity with key %s for taint %v already created for pod %s.", key, taint, pod.Name)
			continue
		}
		// If the pod doesn't have the proper toleration, create TAINT to buy
		if !tolerationsTolerateTaint(pod.Spec.Tolerations, &taint) {
			taintComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_TAINT).
				Key(key).
				Capacity(accessCommodityDefaultCapacity).
				Create()
			if err != nil {
				return nil, err
			}
			visited[key] = true
			glog.V(4).Infof("Created taint commodity with key %s for pod %s", key, pod.GetName())
			taintComms = append(taintComms, taintComm)
		}
	}
	glog.V(4).Infof("Created %d taint commodities for pod %s", len(taintComms), pod.GetName())
	return taintComms, nil
}

// Checks if a taint is tolerated by any of the tolerations.
// The matching follows the rules below:
// (1) Empty toleration.effect means to match all taint effects,
//
//	otherwise taint effect must equal to toleration.effect.
//
// (2) If toleration.operator is 'Exists', it means to match all taint values.
// (3) Empty toleration.key means to match all taint keys.
//
//	If toleration.key is empty, toleration.operator must be 'Exists';
//	this combination means to match all taint values and all taint keys.
func tolerationsTolerateTaint(tolerations []api.Toleration, taint *api.Taint) bool {
	for i := range tolerations {
		if tolerations[i].ToleratesTaint(taint) {
			return true
		}
	}
	return false
}
