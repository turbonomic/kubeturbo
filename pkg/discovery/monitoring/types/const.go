package types

type MonitoringSource string

const (
	KubeletSource    MonitoringSource = "Kubelet"
	ClusterSource    MonitoringSource = "Cluster"
	PrometheusSource MonitoringSource = "Prometheus"
)

type MonitorType string

const (
	ResourceMonitor MonitorType = "ResourceMonitor"
	StateMonitor    MonitorType = "StateMonitor"
)
