package detectors

import (
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/sets"
	"os"
	"regexp"
	"strings"
)

// Master node detection
// list of node name regexps
var masterNodeNamePattern *regexp.Regexp
var masterLabelKeys []*regexp.Regexp
var masterLabelValues []*regexp.Regexp

// Daemon detection
// list of node name regexps
// list of namespace regexps
var daemonPodNamePattern *regexp.Regexp
var daemonNamespacePattern *regexp.Regexp

// HANode detection - list of node roles to be detected
var HANodeRoles sets.String

type NodeLabelEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type MasterNodeDetectors struct {
	NodeNamePatterns []string         `json:"nodeNamePatterns,omitempty"`
	NodeLabels       []NodeLabelEntry `json:"nodeLabels,omitempty"`
}

type DaemonPodDetectors struct {
	Namespaces      []string `json:"namespaces,omitempty"`
	PodNamePatterns []string `json:"podNamePatterns,omitempty"`
}

type HANodeConfig struct {
	NodeRoles []string `json:"nodeRoles,omitempty"`
}

func compileOrDie(pattern string) *regexp.Regexp {
	re, err := regexp.Compile(pattern)
	if err != nil {
		glog.Errorf("Cannot parse regular expression '%s': %v", pattern, err)
		os.Exit(1)
	}
	return re
}

func ValidateAndParseDetectors(masterConfig *MasterNodeDetectors,
	daemonConfig *DaemonPodDetectors, HAConfig *HANodeConfig) {

	// Handle default values when sections are missing
	if masterConfig == nil {
		masterConfig = &MasterNodeDetectors{}
	}
	if daemonConfig == nil {
		daemonConfig = &DaemonPodDetectors{}
	}

	// Pre-compile all regular expressions and ensure that they are valid

	// Master node detection by node name
	masterNodeNamePattern = buildRegexFromList(masterConfig.NodeNamePatterns)

	// Master node detection by label
	masterLabelKeys = make([]*regexp.Regexp, len(masterConfig.NodeLabels))
	masterLabelValues = make([]*regexp.Regexp, len(masterConfig.NodeLabels))
	for i, entry := range masterConfig.NodeLabels {
		masterLabelKeys[i] = compileOrDie(entry.Key)
		masterLabelValues[i] = compileOrDie(entry.Value)
	}

	// Daemon pod detectin by pod name and namespace
	daemonPodNamePattern = buildRegexFromList(daemonConfig.PodNamePatterns)
	daemonNamespacePattern = buildRegexFromList(daemonConfig.Namespaces)

	// Remember HA node roles
	HANodeRoles = sets.NewString()
	if HAConfig != nil {
		for _, role := range HAConfig.NodeRoles {
			HANodeRoles.Insert(role)
		}
	}
	glog.V(2).Infof("##### HARoles to be detected: %v", HANodeRoles)
}

/*
 * Build a regular expression that will match any pattern in the list.  A nil pattern or empty
 * list of patterns will match nothing.
 */
func buildRegexFromList(patterns []string) *regexp.Regexp {
	if patterns == nil || len(patterns) == 0 {
		patterns = make([]string, 0)
	}
	return compileOrDie("(?i)^(" + strings.Join(patterns, "|") + ")$")
}

func IsMasterDetected(nodeName string, labelMap map[string]string) bool {
	result := matches(masterNodeNamePattern, nodeName) || isInMap(labelMap)
	glog.V(4).Infof("IsMasterDetected: %s = %v", nodeName, result)
	return result
}

func IsDaemonDetected(podName, podNamespace string) bool {
	result := matches(daemonPodNamePattern, podName) || matches(daemonNamespacePattern, podNamespace)
	glog.V(4).Infof("IsDaemonDetected: %s/%s = %v", podNamespace, podName, result)
	return result
}

func matches(re *regexp.Regexp, s string) bool {
	return re != nil && re.MatchString(s)
}

func isInMap(m map[string]string) bool {
	for k, v := range m {
		for i, pattern := range masterLabelKeys {
			if pattern.MatchString(k) && masterLabelValues[i].MatchString(v) {
				return true
			}
		}
	}
	return false
}
