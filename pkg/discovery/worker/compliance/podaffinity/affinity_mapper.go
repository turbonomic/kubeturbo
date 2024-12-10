package podaffinity

import (
	"sync"

	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

type MappingType int

const (
	AffinitySrc     MappingType = 1
	AffinityDst     MappingType = 2
	AntiAffinitySrc MappingType = 3
	AntiAffinityDst MappingType = 4
)

type AffinityType struct {
	Sourcetype      MappingType
	DestinationType MappingType
}

var (
	Affinity     AffinityType = AffinityType{Sourcetype: AffinitySrc, DestinationType: AffinityDst}
	AntiAffinity AffinityType = AffinityType{Sourcetype: AntiAffinitySrc, DestinationType: AntiAffinityDst}
)

type ProviderType int

const (
	NodeProvider      ProviderType = 1
	NodeGroupProvider ProviderType = 2
)

type Provider struct {
	ProviderType ProviderType
	ProviderId   string
}

type PodMapping struct {
	// So far the best key name scheme seems to be
	// <topologyKey>|<src-workload-name>|<dest-workload-name>
	// If the given pod does not have a worklaod, we use the podname instead
	// (names are fully qualified)
	// As of now we do not qualify the key with cluster name (TBD: we might need to)
	// In case we hit size limits, these can be replaced by uids
	MappingKey MappingKey

	// if topology key = hostname provider type is node
	//topologyKey string
	// source workload unique name
	//srcWorkload string
	// destination workload unique name
	//dstWorkload string
	// provider is the provider unique name (node or nodegroup)
	Provider Provider
}

type MappingKey struct {
	CommodityKey string
	MappingType  MappingType
}

type SoldKeys struct {
	AffinityKeys     sets.String
	AntiAffinityKeys sets.String
}

type ProviderMapping struct {
	// Key counts if set tell how many pods of that mapping
	// exist on this provider
	KeyCounts map[MappingKey]sets.String
	// keys tell which all keys should be sold by this provider
	Keys SoldKeys
}

// The market will not calculate the price of a commodity if the quantity bought is zero, so we'll
// denote the value "zero" by simply setting it to a value less than 1.
const NONE float64 = 0.0001

type AffinityMapper struct {
	// These fields are internal to AffinityMapper
	sync.RWMutex

	nodeInfoLister NodeInfoLister

	// stores the pod to controller map for reference
	podParentMap map[string]string

	// podId is the key here
	// Also the values emulate a set via a map
	podMappings map[string]map[PodMapping]struct{}

	// providerMappings key is the provider name (i.e. node group or node)
	// TODO: can we see conflicting keys from a node and a nodegroup in the same cluster?
	// For example nodeName => mynode=testing and nodegroup => mynode=testing
	// May be we should fix this (probably use a unique key as provider id and name as a field in ProviderMapping
	// and make this an object to still be able to do efficient lookup)
	providerMappings map[string]*ProviderMapping

	// The SoldKeys list stores the keys for both affinity types ie list of all commodities that should be sold by nodes
	// this is meant for all other nodes which do not have an affinity pod on them
	// All nodes sells each commodity meant for hostnames, except the spread ones
	// the usage value here will be 0
	nodeKeys SoldKeys

	// map of topology key to commodities sold
	// this is used to ensure that node groups that don't have an associated entry in the providerMappings
	// still sell the necessary commodities
	nodeGroupMappingKeys map[string]sets.Set[MappingKey]
}

func NewAffinityMapper(parentMap map[string]string, nodeInfoLister NodeInfoLister) *AffinityMapper {
	return &AffinityMapper{
		podParentMap:         parentMap,
		nodeInfoLister:       nodeInfoLister,
		podMappings:          make(map[string]map[PodMapping]struct{}),
		providerMappings:     make(map[string]*ProviderMapping),
		nodeKeys:             SoldKeys{AffinityKeys: sets.NewString(), AntiAffinityKeys: sets.NewString()},
		nodeGroupMappingKeys: make(map[string]sets.Set[MappingKey]),
	}
}

func (am *AffinityMapper) BuildAffinityMaps(terms []AffinityTerm, srcPodInfo *PodInfo,
	dstPodInfo *PodInfo, node *v1.Node, affinityType AffinityType) bool {
	srcPodId := srcPodInfo.Pod.Namespace + "/" + srcPodInfo.Pod.Name
	dstPodId := dstPodInfo.Pod.Namespace + "/" + dstPodInfo.Pod.Name
	suffix := affinityCommodityKeySuffix(srcPodId, dstPodId, am.podParentMap)

	// These terms already have namespaceSelectors merged into them
	matched := false
	for _, t := range terms {
		if !t.Matches(dstPodInfo.Pod, nil) {
			continue
		}

		matched = true
		meantForNode := false
		commKey := t.TopologyKey + "|" + suffix
		if t.TopologyKey == "kubernetes.io/hostname" {
			meantForNode = true
		}

		srcProviderId := srcPodInfo.Pod.Spec.NodeName
		dstProviderId := node.Name
		providerType := NodeProvider
		if !meantForNode {
			srcLabelValue, srcOk := am.getNodeLabelValue(srcPodId, srcPodInfo.Pod.Spec.NodeName, t.TopologyKey)
			if !srcOk {
				continue
			}
			dstLabelValue, dstOk := node.Labels[t.TopologyKey]
			if !dstOk {
				// This means that we wont be adding any of these mappings for
				// those nodes which has a pod that matched the src pod label selector
				// but does not have the needed topology label
				// TODO: I think this is handled differently in k8s
				// it seems the nodes which do not carry the relevant topology label are
				// considered a match. Verify this and update accordingly
				continue
			}

			srcProviderId = t.TopologyKey + "=" + srcLabelValue
			dstProviderId = t.TopologyKey + "=" + dstLabelValue
			providerType = NodeGroupProvider
		}

		am.AddAffinityPodMappings(srcPodId, dstPodId, commKey, srcProviderId, dstProviderId, providerType, affinityType)
		am.AddAffinityProviderMappings(commKey, srcPodId, dstPodId, srcProviderId, dstProviderId, providerType, affinityType)
		am.AddNodeGroupTopologyKeys(t, dstPodInfo, commKey, affinityType)
	}

	return matched
}

func (am *AffinityMapper) getNodeLabelValue(podId, nodeName, topologyKey string) (string, bool) {
	srcNodeInfo, error := am.nodeInfoLister.Get(nodeName)
	if error != nil {
		glog.V(3).Info(error)
		return "", false
	}
	srcNode := srcNodeInfo.Node()
	if srcNode == nil {
		glog.V(3).Infof("src Node Info is nil for pod %s", podId)
		return "", false
	}
	srcLabelValue, srcOk := srcNode.Labels[topologyKey]
	if !srcOk {
		glog.V(3).Infof("topology label key %s does not exist on node %s", topologyKey, nodeName)
		return "", false
	}
	return srcLabelValue, true
}

func affinityCommodityKeySuffix(srcPodId, dstPodId string, podParents map[string]string) string {
	srcParent, srcOk := podParents[srcPodId]
	srcKeySuffix := srcPodId
	if srcOk {
		srcKeySuffix = srcParent
	}
	dstParent, dstOk := podParents[dstPodId]
	dstKeySuffix := dstPodId
	if dstOk {
		dstKeySuffix = dstParent
	}

	return srcKeySuffix + "|" + dstKeySuffix
}

func (am *AffinityMapper) AddAffinityPodMappings(srcPodId, dstPodId, commKey,
	srcProviderId, dstProviderId string, providerType ProviderType, affinityType AffinityType) {
	am.Lock()
	defer am.Unlock()

	// These mappings emulate a set, entries are unique
	// As of now same mapping (for different pods in the workload)
	// is processed multiple time.
	// TODO: this is a point for optimisation in future
	mSrc, exists := am.podMappings[srcPodId]
	if !exists {
		mSrc = make(map[PodMapping]struct{})
	}
	mSrc[PodMapping{
		MappingKey: MappingKey{
			CommodityKey: commKey,
			MappingType:  affinityType.Sourcetype,
		},
		Provider: Provider{
			ProviderType: providerType,
			ProviderId:   srcProviderId,
		},
	}] = struct{}{}
	am.podMappings[srcPodId] = mSrc

	mDst, exists := am.podMappings[dstPodId]
	if !exists {
		mDst = make(map[PodMapping]struct{})
	}
	mDst[PodMapping{
		MappingKey: MappingKey{
			CommodityKey: commKey,
			MappingType:  affinityType.DestinationType,
		},
		Provider: Provider{
			ProviderType: providerType,
			ProviderId:   dstProviderId,
		},
	}] = struct{}{}
	am.podMappings[dstPodId] = mDst
}

func (am *AffinityMapper) AddAffinityProviderMappings(commKey, srcPodId, dstPodId, srcProviderId, dstProviderId string, providerType ProviderType, affinityType AffinityType) {
	am.Lock()
	defer am.Unlock()

	mappingTypes := []MappingType{affinityType.Sourcetype, affinityType.DestinationType}
	for index, id := range []string{srcProviderId, dstProviderId} {
		mapping, exist := am.providerMappings[id]
		if !exist {
			mapping = &ProviderMapping{
				KeyCounts: make(map[MappingKey]sets.String),
				Keys: SoldKeys{
					AffinityKeys:     sets.NewString(),
					AntiAffinityKeys: sets.NewString(),
				},
			}
		}

		mapKey := MappingKey{
			CommodityKey: commKey,
			MappingType:  mappingTypes[index],
		}

		podList, exists := mapping.KeyCounts[mapKey]
		if !exists {
			podList = sets.NewString()
		}
		switch {
		case mapKey.MappingType == AffinitySrc, mapKey.MappingType == AntiAffinitySrc:
			podList.Insert(srcPodId)
		case mapKey.MappingType == AffinityDst, mapKey.MappingType == AntiAffinityDst:
			podList.Insert(dstPodId)
		}
		mapping.KeyCounts[mapKey] = podList

		switch affinityType {
		case Affinity:
			mapping.Keys.AffinityKeys.Insert(commKey)
		case AntiAffinity:
			mapping.Keys.AntiAffinityKeys.Insert(commKey)
		}

		am.providerMappings[id] = mapping
	}

	if providerType == NodeProvider {
		switch affinityType {
		case Affinity:
			am.nodeKeys.AffinityKeys.Insert(commKey)
		case AntiAffinity:
			am.nodeKeys.AntiAffinityKeys.Insert(commKey)
		}
	}
}

func (am *AffinityMapper) AddNodeGroupTopologyKeys(t AffinityTerm, dstPodInfo *PodInfo, commodityKey string, affinityType AffinityType) {
	if !t.Matches(dstPodInfo.Pod, nil) || t.TopologyKey == "kubernetes.io/hostname" {
		return
	}

	am.Lock()
	defer am.Unlock()
	mappingKey := MappingKey{
		CommodityKey: commodityKey,
		// This simply is used to identify the mapping as affinity or antiaffinity
		// to be translated to relevant commodity. We could as well use affinityType.SrcType here
		MappingType: affinityType.DestinationType,
	}

	if _, exists := am.nodeGroupMappingKeys[t.TopologyKey]; !exists {
		am.nodeGroupMappingKeys[t.TopologyKey] = sets.Set[MappingKey]{}
	}
	am.nodeGroupMappingKeys[t.TopologyKey].Insert(mappingKey)
}

func (am *AffinityMapper) GetPodMappings(podname string) (map[PodMapping]struct{}, bool) {
	mapping, exists := am.podMappings[podname]
	if exists {
		return mapping, true
	}
	return nil, true
}

func (am *AffinityMapper) GetNodeSoldKeys() SoldKeys {
	return am.nodeKeys
}

func (am *AffinityMapper) GetProviderMapping(name string) (*ProviderMapping, bool) {
	mapping, exists := am.providerMappings[name]
	if exists {
		return mapping, true
	}
	return nil, false
}

func (am *AffinityMapper) GetNodeGroupKeys(topologyKey string) (sets.Set[MappingKey], bool) {
	keys, exists := am.nodeGroupMappingKeys[topologyKey]
	if exists {
		return keys, true
	}
	return nil, false
}
