package compliance

import (
	api "k8s.io/client-go/pkg/api/v1"

	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

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
}

func NewTaintTolerationProcessor(nodeAndPodGetter NodeAndPodGetter) (*TaintTolerationProcessor, error) {
	nodes, err := nodeAndPodGetter.GetAllNodes()
	if err != nil {
		return nil, err
	}
	pods, err := nodeAndPodGetter.GetAllPods()
	if err != nil {
		return nil, err
	}

	nodeMap := make(map[string]*api.Node)
	nodeNameToUID := make(map[string]string)
	for _, node := range nodes {
		nodeMap[string(node.UID)] = node
		nodeNameToUID[node.Name] = string(node.UID)
	}

	podMap := make(map[string]*api.Pod)
	for _, pod := range pods {
		podMap[string(pod.UID)] = pod
	}

	return &TaintTolerationProcessor{
		nodes:         nodeMap,
		pods:          podMap,
		nodeNameToUID: nodeNameToUID,
	}, nil
}

// Process takes entityDTOs and add access commodities for VMs and ContainerPods
// based on the taints and tolerations, respectively, in nodes and pods.
func (t *TaintTolerationProcessor) Process(entityDTOs []*proto.EntityDTO) {
	// Preprocess for node taints to create access commodities for each node later.
	taintCollection := getTaintCollection(t.nodes)

	nodeDTOs, podDTOs := retrieveNodeAndPodDTOs(entityDTOs)

	createAccessCommoditiesSold(nodeDTOs, t.nodes, taintCollection)

	createAccessCommoditiesBought(podDTOs, t.pods, t.nodeNameToUID, taintCollection)
}

// Creates access commodities sold by VMs.
func createAccessCommoditiesSold(nodeDTOs []*proto.EntityDTO, nodes map[string]*api.Node, taintCollection map[api.Taint]string) {
	for _, nodeDTO := range nodeDTOs {
		node, ok := nodes[*nodeDTO.Id]
		if !ok {
			glog.Errorf("Unable to find node object with uid %s", *nodeDTO.Id)
			continue
		}

		taintAccessComms, err := createTaintAccessComms(node, taintCollection)
		if err != nil {
			glog.Errorf("Error while process taints for node %s", node.GetName())
			continue
		}

		nodeDTO.CommoditiesSold = append(nodeDTO.CommoditiesSold, taintAccessComms...)
	}
}

// Creates access commodities bought by ContainerPods.
func createAccessCommoditiesBought(podDTOs []*proto.EntityDTO, pods map[string]*api.Pod, nodeNameToUID map[string]string, taintCollection map[api.Taint]string) {
	for _, podDTO := range podDTOs {
		pod, ok := pods[*podDTO.Id]
		if !ok {
			glog.Errorf("Unable to find pod object with uid %s", *podDTO.Id)
			continue
		}

		providerId, ok := nodeNameToUID[pod.Spec.NodeName]
		if !ok {
			glog.Errorf("Unable to find hosting node %s for pod %s", pod.Spec.NodeName, *podDTO.DisplayName)
			continue
		}

		tolerateAccessComms, err := createTolerationAccessComms(pod, taintCollection)
		if err != nil {
			glog.Errorf("Error while process tolerations for pod %s", pod.GetName())
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
			glog.V(2).Infof("Found provider %s for pod %s to buy %d commodities", providerId, podDTO.GetDisplayName(), len(comms))
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
		if *dto.EntityType == proto.EntityDTO_VIRTUAL_MACHINE {
			nodes = append(nodes, dto)
		} else if *dto.EntityType == proto.EntityDTO_CONTAINER_POD {
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
				glog.V(4).Infof("Found taint (comm key = %s): %+v)", taintCollection[taint], taint)
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

	for taint, key := range taintCollection {
		// If the node doesn't contain the taint, create access commodity
		if _, ok := nodeTaints[taint]; !ok {
			accessComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
				Key(key).
				Capacity(accessCommodityDefaultCapacity).
				Create()

			if err != nil {
				return nil, err
			}

			glog.V(4).Infof("Created access commodity with key %s for node %s", key, node.GetName())

			accessComms = append(accessComms, accessComm)
		}
	}

	glog.V(4).Infof("Created %d access commodities for node %s", len(accessComms), node.GetName())

	return accessComms, nil
}

// Creates access commodities bought by ContainerPods based on the taint collection and pod tolerations.
func createTolerationAccessComms(pod *api.Pod, taintCollection map[api.Taint]string) ([]*proto.CommodityDTO, error) {
	accessComms := []*proto.CommodityDTO{}

	for taint, key := range taintCollection {
		// If the pod doesn't have the proper toleration, create access commodity to buy
		if !TolerationsTolerateTaint(pod.Spec.Tolerations, &taint) {
			accessComm, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_VMPM_ACCESS).
				Key(key).
				Capacity(accessCommodityDefaultCapacity).
				Create()

			if err != nil {
				return nil, err
			}

			glog.V(4).Infof("Created access commodity with key %s for pod %s", key, pod.GetName())

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
