package detectors

import (
	"github.com/golang/glog"
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

func ValidateAndParseDetectors(mconfig *MasterNodeDetectors, dconfig *DaemonPodDetectors) error {
	// Handle default values when sections are missing
	if mconfig == nil {
		mconfig = &MasterNodeDetectors{}
	}
	if dconfig == nil {
		dconfig = &DaemonPodDetectors{}
	}

	// Pre-compile all regular expressions and ensure that they are valid

	// Master node detection by node name
	masterNodeNamePattern = buildRegexFromList(mconfig.NodeNamePatterns)

	// Master node detection by label
	masterLabelKeys = make([]*regexp.Regexp, len(mconfig.NodeLabels))
	masterLabelValues = make([]*regexp.Regexp, len(mconfig.NodeLabels))
	for i, entry := range mconfig.NodeLabels {
		masterLabelKeys[i] = regexp.MustCompile(entry.Key)
		masterLabelValues[i] = regexp.MustCompile(entry.Value)
	}

	// Daemon pod detection by pod name and namespace
	daemonPodNamePattern = buildRegexFromList(dconfig.PodNamePatterns)
	daemonNamespacePattern = buildRegexFromList(dconfig.Namespaces)

	return nil
}

/*
 * Build a regular expression that will match any pattern in the list.  A nil pattern or empty
 * list of patterns will match nothing.
 */
func buildRegexFromList(patterns []string) *regexp.Regexp {
	if patterns == nil || len(patterns) == 0 {
		patterns = make([]string, 0)
	}
	return regexp.MustCompile("(?i)^(" + strings.Join(patterns, "|") + ")$")
}

func IsMasterDetected(nodeName string, labelMap map[string]string) bool {
	result := matches(masterNodeNamePattern, nodeName) || isInMap(labelMap)
	glog.V(2).Infof("IsMasterDetected: %v = %v", nodeName, result)
	return result
}

func IsDaemonDetected(podName, podNamespace string) bool {
	result := matches(daemonPodNamePattern, podName) || matches(daemonNamespacePattern, podNamespace)
	glog.V(2).Infof("IsDaemonDetected: %v = %v", podName, result)
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
