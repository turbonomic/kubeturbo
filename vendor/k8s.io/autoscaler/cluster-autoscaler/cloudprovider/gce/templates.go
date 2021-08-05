/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gce

import (
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"strings"

	gce "google.golang.org/api/compute/v1"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/autoscaler/cluster-autoscaler/utils/gpu"
	"k8s.io/autoscaler/cluster-autoscaler/utils/units"
	kubeletapis "k8s.io/kubelet/pkg/apis"

	"github.com/ghodss/yaml"
	klog "k8s.io/klog/v2"
)

// GceTemplateBuilder builds templates for GCE nodes.
type GceTemplateBuilder struct{}

// TODO: This should be imported from sigs.k8s.io/gcp-compute-persistent-disk-csi-driver/pkg/common/constants.go
// This key is applicable to both GCE and GKE
const gceCSITopologyKeyZone = "topology.gke.io/zone"

func (t *GceTemplateBuilder) getAcceleratorCount(accelerators []*gce.AcceleratorConfig) int64 {
	count := int64(0)
	for _, accelerator := range accelerators {
		if strings.HasPrefix(accelerator.AcceleratorType, "nvidia-") {
			count += accelerator.AcceleratorCount
		}
	}
	return count
}

// BuildCapacity builds a list of resource capacities given list of hardware.
func (t *GceTemplateBuilder) BuildCapacity(cpu int64, mem int64, accelerators []*gce.AcceleratorConfig, os OperatingSystem, osDistribution OperatingSystemDistribution, ephemeralStorage int64, pods *int64) (apiv1.ResourceList, error) {
	capacity := apiv1.ResourceList{}
	if pods == nil {
		capacity[apiv1.ResourcePods] = *resource.NewQuantity(110, resource.DecimalSI)
	} else {
		capacity[apiv1.ResourcePods] = *resource.NewQuantity(*pods, resource.DecimalSI)
	}

	capacity[apiv1.ResourceCPU] = *resource.NewQuantity(cpu, resource.DecimalSI)
	memTotal := mem - CalculateKernelReserved(mem, os, osDistribution)
	capacity[apiv1.ResourceMemory] = *resource.NewQuantity(memTotal, resource.DecimalSI)

	if accelerators != nil && len(accelerators) > 0 {
		capacity[gpu.ResourceNvidiaGPU] = *resource.NewQuantity(t.getAcceleratorCount(accelerators), resource.DecimalSI)
	}

	if ephemeralStorage > 0 {
		storageTotal := ephemeralStorage - CalculateOSReservedEphemeralStorage(ephemeralStorage, osDistribution)
		capacity[apiv1.ResourceEphemeralStorage] = *resource.NewQuantity(int64(math.Max(float64(storageTotal), 0)), resource.DecimalSI)
	}

	return capacity, nil
}

// BuildAllocatableFromKubeEnv builds node allocatable based on capacity of the node and
// value of kubeEnv.
// KubeEnv is a multi-line string containing entries in the form of
// <RESOURCE_NAME>:<string>. One of the resources it contains is a list of
// kubelet arguments from which we can extract the resources reserved by
// the kubelet for its operation. Allocated resources are capacity minus reserved.
// If we fail to extract the reserved resources from kubeEnv (e.g it is in a
// wrong format or does not contain kubelet arguments), we return an error.
func (t *GceTemplateBuilder) BuildAllocatableFromKubeEnv(capacity apiv1.ResourceList, kubeEnv string, evictionHard *EvictionHard) (apiv1.ResourceList, error) {
	kubeReserved, err := extractKubeReservedFromKubeEnv(kubeEnv)
	if err != nil {
		return nil, err
	}
	reserved, err := parseKubeReserved(kubeReserved)
	if err != nil {
		return nil, err
	}
	return t.CalculateAllocatable(capacity, reserved, evictionHard), nil
}

// CalculateAllocatable computes allocatable resources subtracting kube reserved values
// and kubelet eviction memory buffer from corresponding capacity.
func (t *GceTemplateBuilder) CalculateAllocatable(capacity apiv1.ResourceList, kubeReserved apiv1.ResourceList, evictionHard *EvictionHard) apiv1.ResourceList {
	allocatable := apiv1.ResourceList{}
	for key, value := range capacity {
		quantity := value.DeepCopy()
		if reservedQuantity, found := kubeReserved[key]; found {
			quantity.Sub(reservedQuantity)
		}
		if key == apiv1.ResourceMemory {
			quantity = *resource.NewQuantity(quantity.Value()-GetKubeletEvictionHardForMemory(evictionHard), resource.BinarySI)
		}
		if key == apiv1.ResourceEphemeralStorage {
			quantity = *resource.NewQuantity(quantity.Value()-int64(GetKubeletEvictionHardForEphemeralStorage(value.Value(), evictionHard)), resource.BinarySI)
		}
		allocatable[key] = quantity
	}
	return allocatable
}

func getKubeEnvValueFromTemplateMetadata(template *gce.InstanceTemplate) (string, error) {
	if template.Properties.Metadata == nil {
		return "", fmt.Errorf("instance template %s has no metadata", template.Name)
	}
	for _, item := range template.Properties.Metadata.Items {
		if item.Key == "kube-env" {
			if item.Value == nil {
				return "", fmt.Errorf("no kube-env content in metadata")
			}
			return *item.Value, nil
		}
	}
	return "", nil
}

// BuildNodeFromTemplate builds node from provided GCE template.
func (t *GceTemplateBuilder) BuildNodeFromTemplate(mig Mig, template *gce.InstanceTemplate, cpu int64, mem int64, pods *int64) (*apiv1.Node, error) {

	if template.Properties == nil {
		return nil, fmt.Errorf("instance template %s has no properties", template.Name)
	}

	node := apiv1.Node{}
	nodeName := fmt.Sprintf("%s-template-%d", template.Name, rand.Int63())

	kubeEnvValue, err := getKubeEnvValueFromTemplateMetadata(template)
	if err != nil {
		return nil, fmt.Errorf("could not obtain kube-env from template metadata; %v", err)
	}

	node.ObjectMeta = metav1.ObjectMeta{
		Name:     nodeName,
		SelfLink: fmt.Sprintf("/api/v1/nodes/%s", nodeName),
		Labels:   map[string]string{},
	}

	// This call is safe even if kubeEnvValue is empty
	os := extractOperatingSystemFromKubeEnv(kubeEnvValue)
	if os == OperatingSystemUnknown {
		return nil, fmt.Errorf("could not obtain os from kube-env from template metadata")
	}

	osDistribution := extractOperatingSystemDistributionFromKubeEnv(kubeEnvValue)
	if osDistribution == OperatingSystemDistributionUnknown {
		return nil, fmt.Errorf("could not obtain os-distribution from kube-env from template metadata")
	}

	var ephemeralStorage int64 = -1
	if !isEphemeralStorageWithInstanceTemplateDisabled(kubeEnvValue) {
		ephemeralStorage, err = getEphemeralStorageFromInstanceTemplateProperties(template.Properties)
		if err != nil {
			klog.Errorf("could not fetch ephemeral storage from instance template. %s", err)
			return nil, err
		}
	}

	capacity, err := t.BuildCapacity(cpu, mem, template.Properties.GuestAccelerators, os, osDistribution, ephemeralStorage, pods)
	if err != nil {
		return nil, err
	}
	node.Status = apiv1.NodeStatus{
		Capacity: capacity,
	}
	var nodeAllocatable apiv1.ResourceList

	if kubeEnvValue != "" {
		// Extract labels
		kubeEnvLabels, err := extractLabelsFromKubeEnv(kubeEnvValue)
		if err != nil {
			return nil, err
		}
		node.Labels = cloudprovider.JoinStringMaps(node.Labels, kubeEnvLabels)

		// Extract taints
		kubeEnvTaints, err := extractTaintsFromKubeEnv(kubeEnvValue)
		if err != nil {
			return nil, err
		}
		node.Spec.Taints = append(node.Spec.Taints, kubeEnvTaints...)

		// Extract Eviction Hard
		evictionHardFromKubeEnv, err := extractEvictionHardFromKubeEnv(kubeEnvValue)
		if err != nil || len(evictionHardFromKubeEnv) == 0 {
			klog.Warning("unable to get evictionHardFromKubeEnv values, continuing without it.")
		}
		evictionHard := ParseEvictionHardOrGetDefault(evictionHardFromKubeEnv)

		if allocatable, err := t.BuildAllocatableFromKubeEnv(node.Status.Capacity, kubeEnvValue, evictionHard); err == nil {
			nodeAllocatable = allocatable
		}
	}

	if nodeAllocatable == nil {
		klog.Warningf("could not extract kube-reserved from kubeEnv for mig %q, setting allocatable to capacity.", mig.GceRef().Name)
		node.Status.Allocatable = node.Status.Capacity
	} else {
		node.Status.Allocatable = nodeAllocatable
	}
	// GenericLabels
	labels, err := BuildGenericLabels(mig.GceRef(), template.Properties.MachineType, nodeName, os)
	if err != nil {
		return nil, err
	}
	node.Labels = cloudprovider.JoinStringMaps(node.Labels, labels)

	// Ready status
	node.Status.Conditions = cloudprovider.BuildReadyConditions()
	return &node, nil
}

// isEphemeralStorageWithInstanceTemplateDisabled will allow bypassing Disk Size of Boot Disk from being
// picked up from Instance Template and used as Ephemeral Storage, in case other type of storage are used
// as ephemeral storage
func isEphemeralStorageWithInstanceTemplateDisabled(kubeEnvValue string) bool {
	v, found, err := extractAutoscalerVarFromKubeEnv(kubeEnvValue, "BLOCK_EPH_STORAGE_BOOT_DISK")
	if err == nil && found && v == "true" {
		return true
	}
	return false
}

func getEphemeralStorageFromInstanceTemplateProperties(instanceProperties *gce.InstanceProperties) (ephemeralStorage int64, err error) {
	if instanceProperties.Disks == nil {
		return 0, fmt.Errorf("unable to get ephemeral storage because instance properties disks is nil")
	}

	for _, disk := range instanceProperties.Disks {
		if disk != nil && disk.InitializeParams != nil {
			if disk.Boot {
				return disk.InitializeParams.DiskSizeGb * units.GiB, nil
			}
		}
	}

	return 0, fmt.Errorf("unable to get ephemeral storage, either no attached disks or no disk with boot=true")
}

// BuildGenericLabels builds basic labels that should be present on every GCE node,
// including hostname, zone etc.
func BuildGenericLabels(ref GceRef, machineType string, nodeName string, os OperatingSystem) (map[string]string, error) {
	result := make(map[string]string)

	if os == OperatingSystemUnknown {
		return nil, fmt.Errorf("unknown operating system passed")
	}

	// TODO: extract it somehow
	result[kubeletapis.LabelArch] = cloudprovider.DefaultArch
	result[apiv1.LabelArchStable] = cloudprovider.DefaultArch
	result[kubeletapis.LabelOS] = string(os)
	result[apiv1.LabelOSStable] = string(os)

	result[apiv1.LabelInstanceType] = machineType
	result[apiv1.LabelInstanceTypeStable] = machineType
	ix := strings.LastIndex(ref.Zone, "-")
	if ix == -1 {
		return nil, fmt.Errorf("unexpected zone: %s", ref.Zone)
	}
	result[apiv1.LabelZoneRegion] = ref.Zone[:ix]
	result[apiv1.LabelZoneRegionStable] = ref.Zone[:ix]
	result[apiv1.LabelZoneFailureDomain] = ref.Zone
	result[apiv1.LabelZoneFailureDomainStable] = ref.Zone
	result[gceCSITopologyKeyZone] = ref.Zone
	result[apiv1.LabelHostname] = nodeName
	return result, nil
}

func parseKubeReserved(kubeReserved string) (apiv1.ResourceList, error) {
	resourcesMap, err := parseKeyValueListToMap(kubeReserved)
	if err != nil {
		return nil, fmt.Errorf("failed to extract kube-reserved from kube-env: %q", err)
	}
	reservedResources := apiv1.ResourceList{}
	for name, quantity := range resourcesMap {
		switch apiv1.ResourceName(name) {
		case apiv1.ResourceCPU, apiv1.ResourceMemory, apiv1.ResourceEphemeralStorage:
			if q, err := resource.ParseQuantity(quantity); err == nil && q.Sign() >= 0 {
				reservedResources[apiv1.ResourceName(name)] = q
			}
		default:
			klog.Warningf("ignoring resource from kube-reserved: %q", name)
		}
	}
	return reservedResources, nil
}

func extractLabelsFromKubeEnv(kubeEnv string) (map[string]string, error) {
	// In v1.10+, labels are only exposed for the autoscaler via AUTOSCALER_ENV_VARS
	// see kubernetes/kubernetes#61119. We try AUTOSCALER_ENV_VARS first, then
	// fall back to the old way.
	labels, found, err := extractAutoscalerVarFromKubeEnv(kubeEnv, "node_labels")
	if err != nil {
		klog.Errorf("error while trying to extract node_labels from AUTOSCALER_ENV_VARS: %v", err)
	}
	if !found {
		labels, err = extractFromKubeEnv(kubeEnv, "NODE_LABELS")
		if err != nil {
			return nil, err
		}
	}
	return parseKeyValueListToMap(labels)
}

func extractTaintsFromKubeEnv(kubeEnv string) ([]apiv1.Taint, error) {
	// In v1.10+, taints are only exposed for the autoscaler via AUTOSCALER_ENV_VARS
	// see kubernetes/kubernetes#61119. We try AUTOSCALER_ENV_VARS first, then
	// fall back to the old way.
	taints, found, err := extractAutoscalerVarFromKubeEnv(kubeEnv, "node_taints")
	if err != nil {
		klog.Errorf("error while trying to extract node_taints from AUTOSCALER_ENV_VARS: %v", err)
	}
	if !found {
		taints, err = extractFromKubeEnv(kubeEnv, "NODE_TAINTS")
		if err != nil {
			return nil, err
		}
	}
	taintMap, err := parseKeyValueListToMap(taints)
	if err != nil {
		return nil, err
	}
	return buildTaints(taintMap)
}

func extractKubeReservedFromKubeEnv(kubeEnv string) (string, error) {
	// In v1.10+, kube-reserved is only exposed for the autoscaler via AUTOSCALER_ENV_VARS
	// see kubernetes/kubernetes#61119. We try AUTOSCALER_ENV_VARS first, then
	// fall back to the old way.
	kubeReserved, found, err := extractAutoscalerVarFromKubeEnv(kubeEnv, "kube_reserved")
	if err != nil {
		klog.Errorf("error while trying to extract kube_reserved from AUTOSCALER_ENV_VARS: %v", err)
	}
	if !found {
		kubeletArgs, err := extractFromKubeEnv(kubeEnv, "KUBELET_TEST_ARGS")
		if err != nil {
			return "", err
		}
		resourcesRegexp := regexp.MustCompile(`--kube-reserved=([^ ]+)`)

		matches := resourcesRegexp.FindStringSubmatch(kubeletArgs)
		if len(matches) > 1 {
			return matches[1], nil
		}
		return "", fmt.Errorf("kube-reserved not in kubelet args in kube-env: %q", kubeletArgs)
	}
	return kubeReserved, nil
}

// OperatingSystem denotes operating system used by nodes coming from node group
type OperatingSystem string

const (
	// OperatingSystemUnknown is used if operating system is unknown
	OperatingSystemUnknown OperatingSystem = ""
	// OperatingSystemLinux is used if operating system is Linux
	OperatingSystemLinux OperatingSystem = "linux"
	// OperatingSystemWindows is used if operating system is Windows
	OperatingSystemWindows OperatingSystem = "windows"

	// OperatingSystemDefault defines which operating system will be assumed if not explicitly passed via AUTOSCALER_ENV_VARS
	OperatingSystemDefault = OperatingSystemLinux
)

func extractOperatingSystemFromKubeEnv(kubeEnv string) OperatingSystem {
	osValue, found, err := extractAutoscalerVarFromKubeEnv(kubeEnv, "os")
	if err != nil {
		klog.Errorf("error while obtaining os from AUTOSCALER_ENV_VARS; %v", err)
		return OperatingSystemUnknown
	}

	if !found {
		klog.Warningf("no os defined in AUTOSCALER_ENV_VARS; using default %v", OperatingSystemDefault)
		return OperatingSystemDefault
	}

	switch osValue {
	case string(OperatingSystemLinux):
		return OperatingSystemLinux
	case string(OperatingSystemWindows):
		return OperatingSystemWindows
	default:
		klog.Errorf("unexpected os=%v passed via AUTOSCALER_ENV_VARS", osValue)
		return OperatingSystemUnknown
	}
}

// OperatingSystemDistribution denotes  distribution of the operating system used by nodes coming from node group
type OperatingSystemDistribution string

const (
	// OperatingSystemDistributionUnknown is used if operating distribution system is unknown
	OperatingSystemDistributionUnknown OperatingSystemDistribution = ""
	// OperatingSystemDistributionUbuntu is used if operating distribution system is Ubuntu
	OperatingSystemDistributionUbuntu OperatingSystemDistribution = "ubuntu"
	// OperatingSystemDistributionWindowsLTSC is used if operating distribution system is Windows LTSC
	OperatingSystemDistributionWindowsLTSC OperatingSystemDistribution = "windows_ltsc"
	// OperatingSystemDistributionWindowsSAC is used if operating distribution system is Windows SAC
	OperatingSystemDistributionWindowsSAC OperatingSystemDistribution = "windows_sac"
	// OperatingSystemDistributionCOS is used if operating distribution system is COS
	OperatingSystemDistributionCOS OperatingSystemDistribution = "cos"
	// OperatingSystemDistributionCOSContainerd is used if operating distribution system is COS Containerd
	OperatingSystemDistributionCOSContainerd OperatingSystemDistribution = "cos_containerd"
	// OperatingSystemDistributionUbuntuContainerd is used if operating distribution system is Ubuntu Containerd
	OperatingSystemDistributionUbuntuContainerd OperatingSystemDistribution = "ubuntu_containerd"

	// OperatingSystemDistributionDefault defines which operating system will be assumed if not explicitly passed via AUTOSCALER_ENV_VARS
	OperatingSystemDistributionDefault = OperatingSystemDistributionCOS
)

func extractOperatingSystemDistributionFromKubeEnv(kubeEnv string) OperatingSystemDistribution {
	osDistributionValue, found, err := extractAutoscalerVarFromKubeEnv(kubeEnv, "os_distribution")
	if err != nil {
		klog.Errorf("error while obtaining os from AUTOSCALER_ENV_VARS; %v", err)
		return OperatingSystemDistributionUnknown
	}

	if !found {
		klog.Warningf("no os-distribution defined in AUTOSCALER_ENV_VARS; using default %v", OperatingSystemDistributionDefault)
		return OperatingSystemDistributionDefault
	}

	switch osDistributionValue {

	case string(OperatingSystemDistributionUbuntu):
		return OperatingSystemDistributionUbuntu
	case string(OperatingSystemDistributionWindowsLTSC):
		return OperatingSystemDistributionWindowsLTSC
	case string(OperatingSystemDistributionWindowsSAC):
		return OperatingSystemDistributionWindowsSAC
	case string(OperatingSystemDistributionCOS):
		return OperatingSystemDistributionCOS
	case string(OperatingSystemDistributionCOSContainerd):
		return OperatingSystemDistributionCOSContainerd
	case string(OperatingSystemDistributionUbuntuContainerd):
		return OperatingSystemDistributionUbuntuContainerd
	default:
		klog.Errorf("unexpected os-distribution=%v passed via AUTOSCALER_ENV_VARS", osDistributionValue)
		return OperatingSystemDistributionUnknown
	}
}

func extractEvictionHardFromKubeEnv(kubeEnvValue string) (map[string]string, error) {
	evictionHardAsString, found, err := extractAutoscalerVarFromKubeEnv(kubeEnvValue, "evictionHard")
	if err != nil {
		klog.Warning("error while obtaining eviction-hard from AUTOSCALER_ENV_VARS; %v", err)
		return nil, err
	}

	if !found {
		klog.Warning("no evictionHard defined in AUTOSCALER_ENV_VARS;")
		return make(map[string]string), nil
	}

	return parseKeyValueListToMap(evictionHardAsString)
}

func extractAutoscalerVarFromKubeEnv(kubeEnv, name string) (value string, found bool, err error) {
	const autoscalerVars = "AUTOSCALER_ENV_VARS"
	autoscalerVals, err := extractFromKubeEnv(kubeEnv, autoscalerVars)
	if err != nil {
		return "", false, err
	}

	if strings.Trim(autoscalerVals, " ") == "" {
		// empty or not present AUTOSCALER_ENV_VARS
		return "", false, nil
	}

	for _, val := range strings.Split(autoscalerVals, ";") {
		val = strings.Trim(val, " ")
		items := strings.SplitN(val, "=", 2)
		if len(items) != 2 {
			return "", false, fmt.Errorf("malformed autoscaler var: %s", val)
		}
		if strings.Trim(items[0], " ") == name {
			return strings.Trim(items[1], " \"'"), true, nil
		}
	}
	klog.Infof("var %s not found in %s: %v", name, autoscalerVars, autoscalerVals)
	return "", false, nil
}

func extractFromKubeEnv(kubeEnv, resource string) (string, error) {
	kubeEnvMap := make(map[string]string)
	err := yaml.Unmarshal([]byte(kubeEnv), &kubeEnvMap)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling kubeEnv: %v", err)
	}
	return kubeEnvMap[resource], nil
}

func parseKeyValueListToMap(kvList string) (map[string]string, error) {
	result := make(map[string]string)
	if len(kvList) == 0 {
		return result, nil
	}
	for _, keyValue := range strings.Split(kvList, ",") {
		kvItems := strings.SplitN(keyValue, "=", 2)
		if len(kvItems) != 2 {
			return nil, fmt.Errorf("error while parsing key-value list, val: %s", keyValue)
		}
		result[kvItems[0]] = kvItems[1]
	}
	return result, nil
}

func buildTaints(kubeEnvTaints map[string]string) ([]apiv1.Taint, error) {
	taints := make([]apiv1.Taint, 0)
	for key, value := range kubeEnvTaints {
		values := strings.SplitN(value, ":", 2)
		if len(values) != 2 {
			return nil, fmt.Errorf("error while parsing node taint value and effect: %s", value)
		}
		taints = append(taints, apiv1.Taint{
			Key:    key,
			Value:  values[0],
			Effect: apiv1.TaintEffect(values[1]),
		})
	}
	return taints, nil
}
