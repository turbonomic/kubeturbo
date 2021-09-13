package util

import (
	"errors"
	"fmt"
	"strings"

	set "github.com/deckarep/golang-set"
	"github.com/golang/glog"
	api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/turbonomic/kubeturbo/pkg/discovery/detectors"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
)

const (
	// NodeLabelRolePrefix and NodeLabelRole
	// There are two node labels that can be used to specify node roles:
	// 1. node-role.kubernetes.io/<role-name>=
	// 2. kubernetes.io/role=<role-name>
	// NodeLabelRolePrefix is a label prefix for node roles
	NodeLabelRolePrefix = "node-role.kubernetes.io/"
	// NodeLabelRole specifies the role of a node
	NodeLabelRole = "kubernetes.io/role"
	// NodeLabelOS and NodeLabelOSBeta specify the OS of a node
	NodeLabelOS     = "kubernetes.io/os"
	NodeLabelOSBeta = "beta.kubernetes.io/os"
	// NodeLabelArch and NodeLabelArchBeta specify the arch of a node
	NodeLabelArch     = "kubernetes.io/arch"
	NodeLabelArchBeta = "beta.kubernetes.io/arch"
	NodePoolAKS = "agentpool"
)

func GetNodeIPForMonitor(node *api.Node, source types.MonitoringSource) (string, error) {
	switch source {
	case types.KubeletSource, types.K8sConntrackSource:
		hostname, ip := node.Name, ""
		for _, addr := range node.Status.Addresses {
			if addr.Type == api.NodeHostName && addr.Address != "" {
				hostname = addr.Address
			}
			if addr.Type == api.NodeInternalIP && addr.Address != "" {
				ip = addr.Address
			}
		}
		if ip != "" {
			return ip, nil
		}
		return "", fmt.Errorf("node %v has no valid hostname and/or IP address: %v %v", node.Name, hostname, ip)
	default:
		return "", errors.New("unsupported monitoring source or monitoring source not provided")
	}
}

func GetNodeOSArch(node *api.Node) (os string, arch string) {
	os = "unknown"
	arch = "unknown"
	labelsMap := node.ObjectMeta.Labels
	defer func() {
		if os == "unknown" {
			glog.Warningf("Unable to parse os of node %s. Current labels %v. Missing labels %s.",
				node.Name, labelsMap, strings.Join([]string{NodeLabelOS, NodeLabelOSBeta}, ","))
		}
		if arch == "unknown" {
			glog.Warningf("Unable to parse arch of node %s. Current labels %v. Missing labels %s.",
				node.Name, labelsMap, strings.Join([]string{NodeLabelArch, NodeLabelArchBeta}, ","))
		}
	}()
	if len(labelsMap) == 0 {
		return
	}
	if val, ok := labelsMap[NodeLabelOS]; ok {
		os = val
	} else if val, ok := labelsMap[NodeLabelOSBeta]; ok {
		os = val
	}
	if val, ok := labelsMap[NodeLabelArch]; ok {
		arch = val
	} else if val, ok := labelsMap[NodeLabelArchBeta]; ok {
		arch = val
	}
	return
}

// NodeIsReady checks if a node is in Ready status.
func NodeIsReady(node *api.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == api.NodeReady {
			return condition.Status == api.ConditionTrue
		}
	}
	glog.Errorf("Node %s does not have Ready status.", node.Name)
	return false
}

// LabelMapFromNodeSelectorString constructs a map of labels from the label selector string specified in the command
// line arguments.
// Example input label selector strings:
//   "kubernetes.io/arch=s390x,beta.kubernetes.io/arch=s390x"
//   "kubernetes.io/arch=s390x,kubernetes.io/arch=arm64"
func LabelMapFromNodeSelectorString(selector string) (map[string]set.Set, error) {
	labelsMap := make(map[string]set.Set)

	if len(selector) == 0 {
		return labelsMap, nil
	}

	labels := strings.Split(selector, ",")
	for _, label := range labels {
		l := strings.Split(label, "=")
		if len(l) != 2 {
			return labelsMap, fmt.Errorf("invalid selector: %v", l)
		}
		key := strings.TrimSpace(l[0])
		value := strings.TrimSpace(l[1])
		if _, exist := labelsMap[key]; !exist {
			labelsMap[key] = set.NewSet()
		}
		labelsMap[key].Add(value)
	}
	return labelsMap, nil
}

func NodeMatchesLabels(node *api.Node, mustMatchLabels map[string]set.Set) bool {
	if len(mustMatchLabels) == 0 {
		return false
	}
	// 1. All keys in the specified label list must match
	// For example:
	//   Given the specified labels on the command line:
	//     --cpufreq-job-exclude-node-labels=kubernetes.io/arch=s390x,beta.kubernetes.io/arch=s390x
	//   Then for a node to match, it must have both kubernetes.io/arch AND beta.kubernetes.io/arch keys
	// 2. For labels with the same key but multiple values, at least one value must match
	// For example:
	//   Given the specified labels on the command line:
	//     --cpufreq-job-exclude-node-labels=kubernetes.io/arch=s390x,kubernetes.io/arch=arm64
	//   Then both s390x and arm64 nodes will match
	for key, values := range mustMatchLabels {
		value, ok := node.Labels[key]
		if !ok || !values.Contains(value) {
			return false
		}
	}
	return true
}

// NodeIsSchedulable checks if a node is schedulable.
func NodeIsSchedulable(node *api.Node) bool {
	return !node.Spec.Unschedulable
}

// NodeIsMaster checks whether the node is a master
func NodeIsMaster(node *api.Node) bool {
	master := detectors.IsMasterDetected(node.Name, node.ObjectMeta.Labels)
	if master {
		glog.V(3).Infof("Node %s is a master", node.Name)
	}
	return master
}

// NodeIsControllable checks whether the node is controllable
func NodeIsControllable(node *api.Node) bool {
	controllable := !NodeIsMaster(node)
	if !controllable {
		glog.V(3).Infof("Node %s is not controllable.", node.Name)
	}
	return controllable
}

func DetectNodeRoles(node *api.Node) sets.String {
	// Parse all roles of a node, and add them to a set
	allRoles := sets.NewString()
	for k, v := range node.Labels {
		switch {
		case strings.HasPrefix(k, NodeLabelRolePrefix):
			if role := strings.TrimPrefix(k, NodeLabelRolePrefix); len(role) > 0 {
				allRoles.Insert(role)
			}
		case k == NodeLabelRole && v != "":
			allRoles.Insert(v)
		}
	}

	// return all the role associated with the node
	return allRoles
}

func DetectNodePools(node *api.Node) sets.String {
	allPools := sets.NewString()
	for k, v := range node.Labels {
		switch {
		case k == NodePoolAKS && v != "":
			allPools.Insert(v)
		}
	}
	return allPools
}

func DetectHARole(node *api.Node) bool {
	nodeRoles := DetectNodeRoles(node)

	isHANode := nodeRoles.Intersection(detectors.HANodeRoles).Len() > 0
	if isHANode {
		glog.V(2).Infof("%s is a HA node and will be marked Non Suspendable.", node.Name)
	}
	return isHANode
}
