package types

type MonitoringSource string

const (
	KubeletSource      MonitoringSource = "Kubelet"
	K8sConntrackSource MonitoringSource = "K8sConntrack"
	ClusterSource      MonitoringSource = "Cluster"
	PrometheusSource   MonitoringSource = "Prometheus"
	DummySource        MonitoringSource = "Dummy" //Testing only
)

type MonitorType string

const (
	ResourceMonitor MonitorType = "ResourceMonitor"
	StateMonitor    MonitorType = "StateMonitor"
	DummyMonitor    MonitorType = "DummyMonitor"
)
