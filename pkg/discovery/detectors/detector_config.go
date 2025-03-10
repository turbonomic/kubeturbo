package detectors

import (
	"os"
	"regexp"
	"strings"

	"github.com/golang/glog"

	"k8s.io/apimachinery/pkg/util/sets"
)

var (
	// Master node detection
	// list of node name regexps
	masterNodeNamePattern *regexp.Regexp
	masterLabelKeys       []*regexp.Regexp
	masterLabelValues     []*regexp.Regexp

	// Daemon detection
	// list of node name regexps
	// list of namespace regexps
	daemonPodNamePattern   *regexp.Regexp
	daemonNamespacePattern *regexp.Regexp

	// System namespaces detection
	// list of system namespace regexps
	systemNamespaces *regexp.Regexp

	// list of operator controlled workload name regexps to be excluded
	// from auto discovered operator controlled container spec groups
	operatorControlledWorkloadsExclusion *regexp.Regexp

	// list of operator controlled workload namespaces regexps to be excluded
	// from auto discovered operator controlled container spec groups
	operatorControlledNamespacesExclusion *regexp.Regexp

	// HANode detection - list of node roles to be detected
	HANodeRoles sets.String
)

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

// Annotations whitelists
var AWContainerSpec string
var AWNamespace string
var AWWorkloadController string

type AnnotationWhitelist struct {
	ContainerSpec      string `json:"containerSpec,omitempty"`
	Namespace          string `json:"namespace,omitempty"`
	WorkloadController string `json:"workloadController,omitempty"`
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
	HAConfig *HANodeConfig, AWConfig *AnnotationWhitelist) {

	// Handle default values when sections are missing
	if masterConfig == nil {
		masterConfig = &MasterNodeDetectors{}
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

	// Remember HA node roles
	HANodeRoles = sets.NewString()
	if HAConfig != nil {
		for _, role := range HAConfig.NodeRoles {
			HANodeRoles.Insert(role)
		}
	}
	glog.V(2).Infof("##### HARoles to be detected: %v", HANodeRoles)

	if AWConfig != nil {
		AWContainerSpec = AWConfig.ContainerSpec
		AWNamespace = AWConfig.Namespace
		AWWorkloadController = AWConfig.WorkloadController
	}

	glog.V(2).Infof("##### Container Spec Annotation Whitelist Regex: %s", AWContainerSpec)
	glog.V(2).Infof("##### Namespace Annotation Whitelist Regex: %s", AWNamespace)
	glog.V(2).Infof("##### Workload Controller Annotation Whitelist Regex: %s", AWWorkloadController)
}

/*
 * Build a regular expression that will match any pattern in the list.  A nil pattern or empty
 * list of patterns will match nothing.
 */
func buildRegexFromList(patterns []string) *regexp.Regexp {
	if len(patterns) == 0 {
		patterns = make([]string, 0)
	}
	return compileOrDie("(?i)^(" + strings.Join(patterns, "|") + ")$")
}

func IsMasterDetected(nodeName string, labelMap map[string]string) bool {
	result := matches(masterNodeNamePattern, nodeName) || isInMap(labelMap)
	glog.V(4).Infof("IsMasterDetected: %s = %v", nodeName, result)
	return result
}

func SetDaemonPods(patterns []string) {
	daemonPodNamePattern = buildRegexFromList(patterns)
}

func SetDaemonNamespaces(patterns []string) {
	daemonNamespacePattern = buildRegexFromList(patterns)
}

func IsDaemonDetected(podName, podNamespace string) bool {
	result := matches(daemonPodNamePattern, podName) || matches(daemonNamespacePattern, podNamespace)
	if result {
		glog.V(4).Infof("IsDaemonDetected: %s/%s = %v", podNamespace, podName, result)
	}
	return result
}

func SetSystemNamespaces(patterns []string) {
	systemNamespaces = buildRegexFromList(patterns)
}

func IsSystemNamespaceDetected(namespace string) bool {
	result := matches(systemNamespaces, namespace)
	if result {
		glog.V(4).Infof("IsSystemNamespaceDetected: %s = %v", namespace, result)
	}
	return result
}

func SetOperatorControlledWorkloadsExclusion(patterns []string) {
	// Exclusion from auto discovered operator controlled container spec groups
	operatorControlledWorkloadsExclusion = buildRegexFromList(patterns)
}

func ExcludedOperatorControlledWorkload(name, namespace string) bool {
	result := matches(operatorControlledWorkloadsExclusion, name)
	if result {
		glog.V(4).Infof("ExcludedFromAutoDiscoveredContainerSpecGroups: workload: %s/%s = %v", namespace, name, result)
	}
	return result
}

func SetOperatorControlledNamespacesExclusion(patterns []string) {
	// Exclusion from auto discovered operator controlled container spec groups
	operatorControlledNamespacesExclusion = buildRegexFromList(patterns)
}

func ExcludedOperatorControlledNamespace(namespace string) bool {
	result := matches(operatorControlledNamespacesExclusion, namespace)
	if result {
		glog.V(4).Infof("ExcludedFromAutoDiscoveredContainerSpecGroups: namespace: %s = %v", namespace, result)
	}
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
