package compliance

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	api "k8s.io/api/core/v1"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const schedAccessCommodityKey string = "schedulable"

type NodeAndPodGetter interface {
	GetAllNodes() ([]*api.Node, error)
	GetAllPods() ([]*api.Pod, error)
}

// TaintTolerationProcessor parses taints defined in nodes and tolerations defined in pods and creates access commodity DTOs,
// sold by VMs and bought by Container Pods.
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

	nodeNameToUID := cluster.NodeNameUIDMap

	nodeToPodsMap := cluster.NodeToRunningPods
	for nodeName, podList := range nodeToPodsMap {
		node := cluster.NodeMap[nodeName]
		nodeMap[node.UID] = node.Node

		for _, pod := range podList {
			podMap[string(pod.UID)] = pod
		}
	}

	return &TaintTolerationProcessor{
		nodes:         nodeMap,
		pods:          podMap,
		nodeNameToUID: nodeNameToUID,
		cluster:       cluster,
	}, nil
}

// Process takes entityDTOs and add access commodities for VMs and ContainerPods
// based on the taints and tolerations, respectively, in nodes and pods.
func (t *TaintTolerationProcessor) Process(entityDTOs []*proto.EntityDTO) {
	// Preprocess for node taints to create access commodities for each node later.
	taintCollection := getTaintCollection(t.nodes)

	nodeDTOs, podDTOs := retrieveNodeAndPodDTOs(entityDTOs)

	t.createAccessCommoditiesSold(nodeDTOs, t.nodes, taintCollection)

	t.createAccessCommoditiesBought(podDTOs, t.pods, t.nodeNameToUID, taintCollection)
}

// Creates access commodities sold by VMs.
func (t *TaintTolerationProcessor) createAccessCommoditiesSold(nodeDTOs []*proto.EntityDTO, nodes map[string]*api.Node,
	taintCollection map[api.Taint]string) {
	for _, nodeDTO := range nodeDTOs {
		node, ok := nodes[*nodeDTO.Id]
		if !ok {
			glog.Errorf("Unable to find node object with uid %s::%s", *nodeDTO.Id, *nodeDTO.DisplayName)
			continue
		}

		// Taints
		taintAccessComms, err := createTaintAccessComms(node, taintCollection)
		if err != nil {
			glog.Errorf("Error while processing taints for node %s", node.GetName())
			continue
		}

		nodeDTO.CommoditiesSold = append(nodeDTO.CommoditiesSold, taintAccessComms...)
	}
}

// Creates access commodities bought by ContainerPods.
func (t *TaintTolerationProcessor) createAccessCommoditiesBought(podDTOs []*proto.EntityDTO, pods map[string]*api.Pod, nodeNameToUID map[string]string, taintCollection map[api.Taint]string) {
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
		tolerateAccessComms, err := createTolerationAccessComms(pod, taintCollection)
		if err != nil {
			glog.Errorf("Error while processing tolerations for pod %s/%s", pod.Namespace, pod.Name)
			continue
		}

		podBuysCommodities(podDTO, tolerateAccessComms, providerId)
	}
}

// Appends accesss commodities to the CommodityBought list in the ContainerPod DTO.
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
	nodes := []*proto.EntityDTO{}
	pods := []*proto.EntityDTO{}

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
			if taint.Effect == api.TaintEffectNoExecute || taint.Effect == api.TaintEffectNoSchedule {
				taintCollection[taint] = taint.Key + "=" + taint.Value + ":" + string(taint.Effect)
				glog.V(2).Infof("Found taint (comm key = %s): %+v)", taintCollection[taint], taint)
			}
		}
	}

	glog.V(2).Infof("Created taint collection with %d taints found", len(taintCollection))

	return taintCollection
}

// Creates access commodities sold by VMs based on the taint collection.
func createTaintAccessComms(node *api.Node, taintCollection map[api.Taint]string) ([]*proto.CommodityDTO, error) {
	accessComms := []*proto.CommodityDTO{}

	taints := node.Spec.Taints
	nodeTaints := make(map[api.Taint]struct{})

	for _, taint := range taints {
		nodeTaints[taint] = struct{}{}
	}
	visited := make(map[string]bool, 0)
	for taint, key := range taintCollection {
		if visited[key] {
			glog.V(4).Infof("Commodity with key %s for taint %v has already been created", key, taint)
			continue
		}
		// If the node doesn't contain the taint, create access commodity
		if _, ok := nodeTaints[taint]; !ok {
			accessComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
				Key(key).
				Capacity(accessCommodityDefaultCapacity).
				Create()

			if err != nil {
				return nil, err
			}
			visited[key] = true
			glog.V(5).Infof("Created access commodity with key %s for node %s", key, node.GetName())

			accessComms = append(accessComms, accessComm)
		}
	}

	glog.V(4).Infof("Created %d access commodities for node %s", len(accessComms), node.GetName())
	return accessComms, nil
}

// Creates access commodities bought by ContainerPods based on the taint collection and pod tolerations.
func createTolerationAccessComms(pod *api.Pod, taintCollection map[api.Taint]string) ([]*proto.CommodityDTO, error) {
	accessComms := []*proto.CommodityDTO{}

	visited := make(map[string]bool, 0)
	for taint, key := range taintCollection {
		if visited[key] {
			glog.V(4).Infof("Commodity with key %s for taint %v has already been created", key, taint)
			continue
		}
		// If the pod doesn't have the proper toleration, create access commodity to buy
		if !TolerationsTolerateTaint(pod.Spec.Tolerations, &taint) {
			accessComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
				Key(key).
				Capacity(accessCommodityDefaultCapacity).
				Create()

			if err != nil {
				return nil, err
			}
			visited[key] = true
			glog.V(5).Infof("Created access commodity with key %s for pod %s", key, pod.GetName())

			accessComms = append(accessComms, accessComm)
		}
	}

	glog.V(4).Infof("Created %d access commodities for pod %s", len(accessComms), pod.GetName())

	return accessComms, nil
}

// Checks if taint is tolerated by any of the tolerations.
func TolerationsTolerateTaint(tolerations []api.Toleration, taint *api.Taint) bool {
	for i := range tolerations {
		if tolerations[i].ToleratesTaint(taint) {
			return true
		}
	}
	return false
}
