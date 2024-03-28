package configs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/golang/glog"
	"github.com/opencontainers/go-digest"
	openshift "github.com/openshift/api/config/v1"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	defaultProbeCategory   = "Cloud Native"
	defaultProbeUICategory = "Cloud Native"
	defaultTargetType      = "Kubernetes"
	versionPathOpenshiftV3 = "/version/openshift"
	versionPathOpenshiftV4 = "/apis/config.openshift.io/v1/clusterversions/version"
)

type K8sTargetInfo struct {
	MasterHost            string
	ServerVersion         string
	ServerVersionOcp      string
	IsOcp                 bool
	ProbeContainerImage   string
	ProbeContainerImageID string
}

type K8sTargetConfig struct {
	ProbeCategory    string `json:"probeCategory,omitempty"`
	ProbeUICategory  string `json:"probeUICategory,omitempty"`
	TargetType       string `json:"targetType,omitempty"`
	TargetIdentifier string `json:"targetName,omitempty"`
	K8sTargetInfo
}

func (config *K8sTargetConfig) ValidateK8sTargetConfig() error {
	// Determine target type
	prefix := defaultTargetType + "-"
	if config.TargetType == "" {
		config.TargetType = defaultTargetType
	} else if !strings.HasPrefix(config.TargetType, prefix) {
		// Prefix targetType with "Kubernetes-"
		config.TargetType = prefix + config.TargetType
	}
	if config.ProbeCategory == "" {
		config.ProbeCategory = defaultProbeCategory
	}
	if config.ProbeUICategory == "" {
		config.ProbeUICategory = defaultProbeUICategory
	}
	// Determine target ID
	if config.TargetIdentifier != "" {
		// targetName is defined, prefix it with Kubernetes is needed
		if !strings.HasPrefix(config.TargetIdentifier, prefix) {
			config.TargetIdentifier = prefix + config.TargetIdentifier
		}
	}
	return nil
}

func (config *K8sTargetConfig) CollectK8sTargetAndProbeInfo(kubeConfig *rest.Config,
	kubeClient *kubernetes.Clientset) {
	// Fill master host
	config.MasterHost = kubeConfig.Host

	// Fill server versions
	serverVersion, err := kubeClient.ServerVersion()
	if err != nil {
		glog.Errorf("Unable to get Kubernetes version info: %v", err)
	} else {
		config.ServerVersion = serverVersion.String()
		glog.V(2).Infof("Kubernetes version: %v", serverVersion)
	}

	// Initialize Openshift version. Set to empty string if not exists
	config.OpenShiftVersion(kubeClient)
	glog.V(2).Infof("Is OpenShift: %v", config.IsOcp)

	// Get kubeturbo container image and image ID
	podName := os.Getenv("HOSTNAME")
	if len(podName) == 0 {
		glog.Warning("Could not determine pod name: environment variable HOSTNAME is missing.")
		return
	}
	glog.V(2).Infof("Pod name for kubeturbo: %v", podName)
	fieldSelector, err := fields.ParseSelector("metadata.name=" + podName)
	if err != nil {
		glog.Warningf("Could not parse field selector: %v", err)
		return
	}
	podList, err := kubeClient.CoreV1().
		Pods(api.NamespaceAll).
		List(context.TODO(), metav1.ListOptions{
			FieldSelector: fieldSelector.String(),
		})
	if err != nil {
		glog.Warningf("Could not get pod with name %v: %v", podName, err)
		return
	}
	if len(podList.Items) != 1 {
		glog.Warningf("Could not get pod with name %v: no pod is returned from API.", podName)
		return
	}
	pod := podList.Items[0]
	containerStatuses := pod.Status.ContainerStatuses
	for _, containerStatus := range containerStatuses {
		image := containerStatus.Image
		if !strings.Contains(image, "kubeturbo") {
			continue
		}
		config.ProbeContainerImage = image
		imageIDString := strings.Split(containerStatus.ImageID, "@")
		if len(imageIDString) < 2 {
			glog.Warningf("Could not find a digest string from imageID %v", imageIDString)
			break
		}
		digestString := imageIDString[len(imageIDString)-1]
		_, err := digest.Parse(digestString)
		if err != nil {
			glog.Warningf("Could not validate the digest string %v: %v", digestString, err)
			break
		}
		config.ProbeContainerImageID = digestString
		glog.V(2).Infof("Kubeturbo container image: %v", image)
		glog.V(2).Infof("kubeturbo container image ID: %v", digestString)
		break
	}
}

// Initialize OpenShift version
// Set it to empty string, if not exists
func (config *K8sTargetConfig) OpenShiftVersion(kubeClient *kubernetes.Clientset) {
	restClient := kubeClient.DiscoveryClient.RESTClient()

	serverVersionOcp, isOcp := openShiftVersion4(restClient)
	if isOcp {
		config.ServerVersionOcp = serverVersionOcp
		config.IsOcp = isOcp
		return
	}

	serverVersionOcp, isOcp = openShiftVersion3(restClient)
	config.ServerVersionOcp = serverVersionOcp
	config.IsOcp = isOcp
}

// Retrieve OpenShift 3 version
// Return version if exists, else empty string; Is cluser OpenShift
func openShiftVersion3(restClient rest.Interface) (string, bool) {
	bytes, err := restClient.Get().AbsPath(versionPathOpenshiftV3).DoRaw(context.TODO())
	if err != nil {
		glog.V(2).Infof("No OpenShift version 3 found: %v. %v", err, string(bytes))
		return "", false
	}

	var versionInfo version.Info
	err = json.Unmarshal(bytes, &versionInfo)
	if err != nil {
		glog.Errorf("Unable to parse OpenShift 3 version info: %v", err)
		return "", true
	}

	glog.V(2).Infof("OpenShift version: %v", versionInfo)
	return versionInfo.String(), true
}

// Retrieve OpenShift 4 version
// Return version if exists, else empty string; Is cluser OpenShift
func openShiftVersion4(restClient rest.Interface) (string, bool) {
	bytes, err := restClient.Get().AbsPath(versionPathOpenshiftV4).DoRaw(context.TODO())
	if err != nil {
		glog.V(2).Infof("No OpenShift version 4 found: %v. %v", err, string(bytes))
		return "", false
	}

	var clusterVersion openshift.ClusterVersion
	err = json.Unmarshal(bytes, &clusterVersion)
	if err != nil {
		glog.Errorf("Unable to parse OpenShift 4 version info: %v", err)
		return "", true
	}

	updateHistory := clusterVersion.Status.History
	if len(updateHistory) == 0 {
		glog.Errorf("OpenShift 4 version info has no history")
		return "", true
	}

	lastUpdate := updateHistory[0]
	if lastUpdate.State != openshift.CompletedUpdate {
		version := fmt.Sprintf("Upgrading to %s", lastUpdate.Version)
		glog.V(2).Infof("OpenShift 4 version last state is : %v", lastUpdate.State)
		glog.V(2).Infof("OpenShift version: %v", version)
		return version, true
	}

	glog.V(2).Infof("OpenShift version: %v", lastUpdate.Version)
	return lastUpdate.Version, true
}
