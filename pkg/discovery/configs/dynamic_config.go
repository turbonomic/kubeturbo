package configs

import (
	"fmt"
	"strconv"
	"strings"
	"syscall"

	"github.com/golang/glog"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/detectors"
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/mediationcontainer"
)

// Exported constants
const (
	// AutoReload configuration file path
	AutoReloadConfigFilePath = "/etc/kubeturbo"
	// AutoReload configuration file name
	AutoReloadConfigFileName = "turbo-autoreload.config"
	// default value of the minimum number of nodes allowed in the node pool.
	DefaultMinNodePoolSize = 1
	// default value of the maximum number of nodes allowed in the node pool.
	DefaultMaxNodePoolSize = 1000

	// Config paths are "." saparated json paths into the configuration
	// minimum number of nodes in the node pool in the ConfigMap file turbo-autoreload.config.
	MinNodesConfigPath = "nodePoolSize.min"
	// maximum number of nodes in the node pool in the ConfigMap file turbo-autoreload.config.
	MaxNodesConfigPath = "nodePoolSize.max"
	// comma separated regex patterns selecting namespaces which should be used to
	// discover workloads that are in system namespaces. This is then used to autodiscover
	// system namespaced container spec groups
	SystemNamespacesPath = "systemWorkloadDetectors.namespacePatterns"
	// comma separated regex patterns selecting workloads to be EXCLUDED from autodiscovered
	// operator controlled container spec groups
	OperatorControlledWorkloadsExclusionPath = "exclusionDetectors.operatorControlledWorkloadsPatterns"
	// comma separated regex patterns selecting namespaces to be EXCLUDED from autodiscovered
	// operator controlled container spec groups
	OperatorControlledNamespacesExclusionPath = "exclusionDetectors.operatorControlledNamespacePatterns"
	// WireMock mode path
	WireMockModePath = "wiremock.enabled"
	// WireMock service URL path
	WireMockSvcUrlPath = "wiremock.url"
	// Default WireMock service URL
	DefaultWireMockSvcUrl = "http://wiremock:8080"
	// comma separated regex patterns selecting all pods in the namespace as
	// daemonsets related pods
	DaemonPodNamePatterns = "daemonPodDetectors.podNamePatterns"
	// comma separated regex patterns selecting pods as daemonsets related pods
	DaemonNamespacePatterns = "daemonPodDetectors.namespaces"
	// Wait delay in milliseconds to send discovery chunk data to throttle overall data transfer to server. See default below.
	ChunkSendDelayMillisPath = "discovery.chunkSendDelayMillis"
	// The number of object to include in each chunk of discovery data sent to the server. See default below.
	NumObjectsPerChunkPath = "discovery.numObjectsPerChunk"
)

func init() {
	viper.AddConfigPath(AutoReloadConfigFilePath)
	viper.SetConfigType("json")
	viper.SetConfigName(AutoReloadConfigFileName)
	viper.ReadInConfig()
}

// helper function getNodePoolSizeConfigValue retrieves the node pool size configuration value based on the provided configuration key.
// The function uses the provided `getKeyStringVal` function to obtain the configuration value as a string.
// If the retrieved value is a valid non-negative integer, it is returned. Otherwise, the `defaultValue` is returned.
//
// Parameters:
// - configKey: The key for which the configuration value needs to be retrieved.
// - getKeyStringVal: A function that takes a configuration key and returns a string representation of the value.
// - defaultValue: The default value to be used if the retrieved value is invalid or unavailable.
//
// Returns:
// An integer representing the retrieved configuration value or the default value if the value is invalid or missing.
func GetNodePoolSizeConfigValue(configKey string, getKeyStringVal func(string) string, defaultValue int) int {
	valStr := getKeyStringVal(configKey)
	glog.V(4).Infof("key: '%s' with string value %q", configKey, valStr)

	if valStr == "" {
		glog.V(4).Infof("Empty value for %s in the configuration, using default value %d", configKey, defaultValue)
		return defaultValue
	}

	valInt, err := strconv.Atoi(valStr)
	if err != nil || valInt < 0 {
		glog.Errorf("Invalid %s value: %q specified, using default value %d", configKey, valStr, defaultValue)
		return defaultValue
	}

	return valInt
}

// updateLoggingLevel updates the logging verbosity level based on configuration.
func UpdateLoggingLevel() {
	newLoggingLevel := viper.GetString("logging.level")
	currentLoggingLevel := pflag.Lookup("v").Value.String()

	if newLoggingLevel != "" && newLoggingLevel != currentLoggingLevel {
		if newLogVInt, err := strconv.Atoi(newLoggingLevel); err != nil || newLogVInt < 0 {
			glog.Errorf("Invalid log verbosity %v in the autoreload config file", newLoggingLevel)
		} else {
			err := pflag.Lookup("v").Value.Set(newLoggingLevel)
			if err != nil {
				glog.Errorf("Can't apply the new logging level setting due to the error:%v", err)
			} else {
				glog.V(1).Infof("Logging level is changed from %v to %v", currentLoggingLevel, newLoggingLevel)
			}
		}
	}
}

func UpdateNodePoolConfig(currentMinNodes *int, currentMaxNodes *int) {
	newMinNodes := logCurrentValueAndGetValue(MinNodesConfigPath, *currentMinNodes, DefaultMinNodePoolSize)
	newMaxNodes := logCurrentValueAndGetValue(MaxNodesConfigPath, *currentMaxNodes, DefaultMaxNodePoolSize)

	if newMinNodes > newMaxNodes {
		glog.Errorf("Cannot make cluster node pool size configuration change since %s value: %d is larger than %s value: %d",
			MinNodesConfigPath, newMinNodes, MaxNodesConfigPath, newMaxNodes)
	} else {
		updateValueAndLog(MinNodesConfigPath, currentMinNodes, newMinNodes)
		updateValueAndLog(MaxNodesConfigPath, currentMaxNodes, newMaxNodes)
	}
}

func logCurrentValueAndGetValue(configKey string, currentValue int, defaultValue int) int {
	glog.V(1).Infof("Cluster %s current value is %v", configKey, currentValue)
	return GetNodePoolSizeConfigValue(configKey, viper.GetString, defaultValue)
}

func updateValueAndLog(configKey string, currentValue *int, newValue int) {
	if *currentValue != newValue {
		glog.V(1).Infof("Cluster %s changed from %v to %v", configKey, *currentValue, newValue)
		*currentValue = newValue
	}
}

func UpdateSystemNamespaceDetectors() {
	patterns := viper.GetStringSlice(SystemNamespacesPath)
	detectors.SetSystemNamespaces(patterns)
	glog.Infof("System Namespace detectors set to: %v", patterns)
}

func UpdateOperatorControlledWorkloadsExclusion() {
	patterns := viper.GetStringSlice(OperatorControlledWorkloadsExclusionPath)
	detectors.SetOperatorControlledWorkloadsExclusion(patterns)
	glog.Infof("Operator controlled workload exclusion set to: %v", patterns)
}

func UpdateOperatorControlledNamespacesExclusion() {
	patterns := viper.GetStringSlice(OperatorControlledNamespacesExclusionPath)
	detectors.SetOperatorControlledNamespacesExclusion(patterns)
	glog.Infof("Operator controlled namespace exclusion set to: %v", patterns)
}

func GetCurrentWireMockMode() (bool, string) {
	if viper.GetBool(WireMockModePath) {
		wiremockSvcAddr := viper.GetString(WireMockSvcUrlPath)
		if wiremockSvcAddr == "" {
			wiremockSvcAddr = DefaultWireMockSvcUrl
		} else if !strings.HasPrefix(wiremockSvcAddr, "http") {
			wiremockSvcAddr = fmt.Sprintf("http://%v", wiremockSvcAddr)
		}
		return true, wiremockSvcAddr
	}
	return false, ""
}

// Check and update if the WireMock mode is set
func UpdateWireMockMode(oldModeFlag bool) {
	newModeFlag, _ := GetCurrentWireMockMode()
	glog.V(3).Infof("Update the WireMock mode from %v to %v", oldModeFlag, newModeFlag)
	// We don't need to save the newModeFlag, Kubeturbo will be restarted and new Kubeturbo will recognize the newModeFlag
	if newModeFlag != oldModeFlag {
		//WireMock mode is flipped, have to restart Kubeturbo container to make it take effect
		glog.Infof("WireMock mode flag is changed from %v to %v, Kubeturbo will exit", oldModeFlag, newModeFlag)
		syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
	}
}

func UpdateDaemonPods() {
	patterns := viper.GetStringSlice(DaemonPodNamePatterns)
	detectors.SetDaemonPods(patterns)
	glog.Infof("Container pod daemonConfig Regex set to: %v", patterns)
}

func UpdateDaemonNamespaces() {
	patterns := viper.GetStringSlice(DaemonNamespacePatterns)
	detectors.SetDaemonNamespaces(patterns)
	glog.Infof("Namespace daemonConfig Regex set to: %v", patterns)
}

func UpdateChunkSendDelayMillis() {
	patterns := viper.GetInt(ChunkSendDelayMillisPath)
	glog.Infof("Discovery ChunkSendDelayMillis set to: %v", patterns)
}

func GetChunkSendDelayMillis() int {
	chunkSendDelayMillis := viper.GetInt(ChunkSendDelayMillisPath)
	if chunkSendDelayMillis < 0 || chunkSendDelayMillis > mediationcontainer.MaxChunkSendDelayMillis {
		glog.Warningf("Invalid configuration for '%v'. Returning default: %v", ChunkSendDelayMillisPath, mediationcontainer.DefaultChunkSendDelayMillis)
		return mediationcontainer.DefaultChunkSendDelayMillis
	}
	return chunkSendDelayMillis
}

func UpdateNumObjectsPerChunk() {
	patterns := viper.GetInt(NumObjectsPerChunkPath)
	glog.Infof("Discovery NumObjectsPerChunk set to: %v", patterns)
}

func GetNumObjectsPerChunk() int {
	numObjectsPerChunk := viper.GetInt(NumObjectsPerChunkPath)
	if numObjectsPerChunk <= 0 || numObjectsPerChunk > mediationcontainer.MaxNumObjectsPerChunk {
		glog.Warningf("Invalid configuration for '%v'. Returning default: %v", NumObjectsPerChunkPath, mediationcontainer.DefaultNumObjectsPerChunk)
		return mediationcontainer.DefaultNumObjectsPerChunk
	}
	return numObjectsPerChunk
}
