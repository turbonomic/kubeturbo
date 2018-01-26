package configs

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
	"k8s.io/client-go/kubernetes"
)

type ProbeConfig struct {
	// A correct stitching property type is the prerequisite for stitching process.
	StitchingPropertyType stitching.StitchingPropertyType

	// Config for one or more monitoring clients
	MonitoringConfigs []monitoring.MonitorWorkerConfig

	// Rest Client for the kubernetes server API
	ClusterClient *kubernetes.Clientset
	// Rest Client for the kubelet module in each node
	NodeClient *kubeclient.KubeletClient
}
