package util

import (
	"errors"
	"fmt"
	"strings"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"

	"github.com/turbonomic/kubeturbo/pkg/discovery/detectors"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/types"
	api "k8s.io/api/core/v1"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/sets"
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
		return "", fmt.Errorf("Node %v has no valid hostname and/or IP address: %v %v", node.Name, hostname, ip)
	default:
		return "", errors.New("Unsupported monitoring source or monitoring source not provided")
	}
}

// Check if a node is in Ready status.
func NodeIsReady(node *api.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == api.NodeReady {
			return condition.Status == api.ConditionTrue
		}
	}
	glog.Errorf("Node %s does not have Ready status.", node.Name)
	return false
}

func NodeMatchesLabels(node *api.Node, defaultLabels, mustMatchLabels map[string]string) bool {
	if len(mustMatchLabels) > 0 {
		// All labels must match
		for k, v := range mustMatchLabels {
			value, ok := node.Labels[k]
			if !ok {
				return false
			}
			if value != v {
				return false
			}
		}
		return true
	}

	// Any label matched from default is a match
	for k, v := range defaultLabels {
		value, ok := node.Labels[k]
		if ok && value == v {
			return true
		}
	}
	return false
}

// Check if a node is schedulable.
func NodeIsSchedulable(node *api.Node) bool {
	return !node.Spec.Unschedulable
}

func GetNodeIP(node *api.Node) (string, error) {
	ip := ""
	for _, addr := range node.Status.Addresses {
		if addr.Type == api.NodeInternalIP && addr.Address != "" {
			ip = addr.Address
		}
	}
	if ip != "" {
		return ip, nil
	}
	return "", fmt.Errorf("node %v has no valid hostname and/or IP address: %v", node.Name, ip)
}

// Check whether the node is a master
func NodeIsMaster(node *api.Node) bool {
	master := detectors.IsMasterDetected(node.Name, node.ObjectMeta.Labels)
	if master {
		glog.V(3).Infof("Node %s is a master", node.Name)
	}
	return master
}

// Returns whether the node is controllable
func NodeIsControllable(node *api.Node) bool {
	controllable := !NodeIsMaster(node)
	if !controllable {
		glog.V(3).Infof("Node %s is not controllable.", node.Name)
	}
	return controllable
}

// There are two node labels that can be used to specify node roles:
// 1. node-role.kubernetes.io/<role-name>=
// 2. kubernetes.io/role=<role-name>
const (
	// labelNodeRolePrefix is a label prefix for node roles
	labelNodeRolePrefix = "node-role.kubernetes.io/"
	// nodeLabelRole specifies the role of a node
	nodeLabelRole = "kubernetes.io/role"
)

func DetectNodeRoles(node *api.Node) sets.String {
	// Parse all roles of a node, and add them to a set
	allRoles := sets.NewString()
	for k, v := range node.Labels {
		switch {
		case strings.HasPrefix(k, labelNodeRolePrefix):
			if role := strings.TrimPrefix(k, labelNodeRolePrefix); len(role) > 0 {
				allRoles.Insert(role)
			}
		case k == nodeLabelRole && v != "":
			allRoles.Insert(v)
		}
	}

	// return all the role associated with the node
	return allRoles
}

func DetectHARole(node *api.Node) bool {
	nodeRoles := DetectNodeRoles(node)

	isHANode := nodeRoles.Intersection(detectors.HANodeRoles).Len() > 0
	if isHANode {
		glog.V(2).Infof("%s is a HA node and will be marked Non Suspendable.", node.Name)
	}
	return isHANode
}

// GetNodeCPUFrequency gets hosting node CPU frequency from EntityMetricSink.
func GetNodeCPUFrequency(nodeName string, metricsSink *metrics.EntityMetricSink) (float64, error) {
	cpuFrequencyUID := metrics.GenerateEntityStateMetricUID(metrics.NodeType, nodeName, metrics.CpuFrequency)
	cpuFrequencyMetric, err := metricsSink.GetMetric(cpuFrequencyUID)
	if err != nil {
		err := fmt.Errorf("failed to get cpu frequency from sink for node %s: %v", nodeName, err)
		return 0.0, err
	}
	cpuFrequency := cpuFrequencyMetric.GetValue().(float64)
	return cpuFrequency, nil
}
