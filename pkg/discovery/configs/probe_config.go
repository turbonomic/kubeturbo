package configs

import (
	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/monitoring"
	"github.ibm.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	"github.ibm.com/turbonomic/kubeturbo/pkg/kubeclient"
)

type ProbeConfig struct {
	// A correct stitching property type is the prerequisite for stitching process.
	StitchingPropertyType stitching.StitchingPropertyType

	// Config for one or more monitoring clients
	MonitoringConfigs []monitoring.MonitorWorkerConfig

	// ClusterScraper contains rest client (ClientSet) and dynamic client (DynamicClient) for the kubernetes server API
	ClusterScraper *cluster.ClusterScraper
	// Rest Client for the kubelet module in each node
	NodeClient *kubeclient.KubeletClient
}
