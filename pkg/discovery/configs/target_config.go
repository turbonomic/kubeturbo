package configs

import (
	"encoding/json"
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
	defaultTargetType      = "Kubernetes"
	versionPathOpenshiftV3 = "/version/openshift"
	versionPathOpenshiftV4 = "/apis/config.openshift.io/v1/clusterversions/version"
)

type K8sTargetInfo struct {
	MasterHost            string
	ServerVersions        []string
	ProbeContainerImage   string
	ProbeContainerImageID string
}

type K8sTargetConfig struct {
	ProbeCategory    string `json:"probeCategory,omitempty"`
	TargetType       string `json:"targetType,omitempty"`
	TargetIdentifier string `json:"targetName,omitempty"`
	K8sTargetInfo
}

func (config *K8sTargetConfig) ValidateK8sTargetConfig() error {
	// Determine target type
	prefix := defaultTargetType + "-"
	if config.TargetType == "" {
		config.TargetType = config.TargetIdentifier
	}
	// Prefix targetType with Kubernetes if needed
	if !strings.HasPrefix(config.TargetType, prefix) {
		config.TargetType = prefix + config.TargetType
	}
	if config.ProbeCategory == "" {
		config.ProbeCategory = defaultProbeCategory
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
		config.ServerVersions = append(config.ServerVersions, serverVersion.String())
		glog.V(2).Infof("Kubernetes version: %v", serverVersion)
	}

	// Check Openshift version, if exists
	restClient := kubeClient.DiscoveryClient.RESTClient()
	if bytes, err := restClient.Get().AbsPath(versionPathOpenshiftV3).DoRaw(); err == nil {
		// Openshift 3
		var versionInfo version.Info
		if err := json.Unmarshal(bytes, &versionInfo); err == nil {
			glog.V(2).Infof("Openshift version: %v", versionInfo)
			OCPVersion := "OCP " + versionInfo.String()
			config.ServerVersions = append(config.ServerVersions, OCPVersion)
		}
	} else if bytes, err := restClient.Get().AbsPath(versionPathOpenshiftV4).DoRaw(); err == nil {
		// Openshift 4
		var clusterVersion openshift.ClusterVersion
		if err := json.Unmarshal(bytes, &clusterVersion); err == nil {
			updateHistory := clusterVersion.Status.History
			if len(updateHistory) > 0 {
				lastUpdate := updateHistory[0]
				if lastUpdate.State == openshift.CompletedUpdate {
					glog.V(2).Infof("Openshift version: %v", lastUpdate.Version)
					OCPVersion := "OCP " + lastUpdate.Version
					config.ServerVersions = append(config.ServerVersions, OCPVersion)
				}
			}
		}
	}

	// Get kubeturbo container image and image ID
	podName := os.Getenv("HOSTNAME")
	if len(podName) == 0 {
		glog.Warning("Failed to determine pod name: environment variable HOSTNAME is missing.")
		return
	}
	glog.V(2).Infof("Pod name for kubeturbo: %v", podName)
	fieldSelector, err := fields.ParseSelector("metadata.name=" + podName)
	if err != nil {
		glog.Warningf("Failed to parse field selector: %v", err)
		return
	}
	podList, err := kubeClient.CoreV1().
		Pods(api.NamespaceAll).
		List(metav1.ListOptions{
			FieldSelector: fieldSelector.String(),
		})
	if err != nil {
		glog.Warningf("Failed to get pod with name %v: %v", podName, err)
		return
	}
	if len(podList.Items) != 1 {
		glog.Warningf("Failed to get pod with name %v: no pod is returned from API.", podName)
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
			glog.Warningf("Failed to find a digest string from imageID %v", imageIDString)
			break
		}
		digestString := imageIDString[len(imageIDString)-1]
		_, err := digest.Parse(digestString)
		if err != nil {
			glog.Warningf("Failed to validate the digest string %v: %v", digestString, err)
			break
		}
		config.ProbeContainerImageID = digestString
		glog.V(2).Infof("Kubeturbo container image: %v", image)
		glog.V(2).Infof("kubeturbo container image ID: %v", digestString)
		break
	}
}
