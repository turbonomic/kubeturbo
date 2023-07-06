package master

import (
	"fmt"
	"testing"

	client "k8s.io/client-go/kubernetes"

	"github.com/golang/glog"
	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// All cpu metrics are generated in millicores
var expectedMetrics = map[string]float64{
	// Node metrics
	"Node-mynode-CPU-Capacity":           2000,
	"Node-mynode-Memory-Capacity":        8.388608e+06,
	"Node-mynode-CPURequest-Capacity":    1900,
	"Node-mynode-MemoryRequest-Capacity": 7.340032e+06,
	"Node-mynode-CPURequest-Used":        261,
	"Node-mynode-MemoryRequest-Used":     262144,

	// Pod metrics
	"Pod-default/mypod-CPU-Capacity":       2000,
	"Pod-default/mypod-Memory-Capacity":    8.388608e+06,
	"Pod-default/mypod-CPURequest-Used":    261,
	"Pod-default/mypod-MemoryRequest-Used": 262144,

	// Container metrics
	"Container-default/mypod/twitter-cass-tweet-CPU-Capacity":            250,
	"Container-default/mypod/twitter-cass-tweet-Memory-Capacity":         262144,
	"Container-default/mypod/twitter-cass-tweet-CPURequest-Capacity":     250,
	"Container-default/mypod/twitter-cass-tweet-MemoryRequest-Capacity":  262144,
	"Container-default/mypod/istio-proxy-CPU-Capacity":                   2000,
	"Container-default/mypod/istio-proxy-Memory-Capacity":                8.388608e+06,
	"Container-default/mypod/istio-proxy-CPURequest-Capacity":            10,
	"Container-default/mypod/istio-proxy-MemoryRequest-Capacity":         0,
	"Container-default/mypod/twitter-cass-tweet-CPULimitQuota-Used":      250,
	"Container-default/mypod/twitter-cass-tweet-MemoryLimitQuota-Used":   262144,
	"Container-default/mypod/twitter-cass-tweet-CPURequestQuota-Used":    250,
	"Container-default/mypod/twitter-cass-tweet-MemoryRequestQuota-Used": 262144,
	"Container-default/mypod/istio-proxy-CPULimitQuota-Used":             2000,
	"Container-default/mypod/istio-proxy-CPURequestQuota-Used":           10,
	"Container-default/mypod/istio-proxy-MemoryLimitQuota-Used":          8.388608e+06,
	"Container-default/mypod/istio-proxy-MemoryRequestQuota-Used":        0,
	"Container-default/mypod/filebeat-sidecar-CPULimitQuota-Used":        1,
	"Container-default/mypod/filebeat-sidecar-CPURequestQuota-Used":      1,
	"Container-default/mypod/filebeat-sidecar-MemoryLimitQuota-Used":     8.388608e+06,
	"Container-default/mypod/filebeat-sidecar-MemoryRequestQuota-Used":   0,
}

func genCPUQuantity(cores float32) resource.Quantity {
	cpuTime := cores * 1000
	result, _ := resource.ParseQuantity(fmt.Sprintf("%fm", cpuTime))
	glog.V(3).Infof("result = %+v", result)
	return result
}

func genMemQuantity(numKB int64) resource.Quantity {
	result, _ := resource.ParseQuantity(fmt.Sprintf("%dKi", numKB))
	glog.V(3).Infof("result = %+v", result)
	return result
}

func buildResource(cores float32, numMB int64) api.ResourceList {
	return api.ResourceList{
		api.ResourceCPU:    genCPUQuantity(cores),
		api.ResourceMemory: genMemQuantity(numMB * 1024),
	}
}

func mockContainer(name string, requestCores, limitCores float32,
	requestMB, limitMB int64) api.Container {
	container := api.Container{
		Name: name,
		Resources: api.ResourceRequirements{
			Limits:   buildResource(limitCores, limitMB),
			Requests: buildResource(requestCores, requestMB),
		},
	}
	return container
}

func mockOwnerReference() (r metav1.OwnerReference) {
	r = metav1.OwnerReference{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Name:       "twitter-cass-tweet",
		UID:        "e2b4720e-1b59-11e9-a464-0e8e22e09e66",
	}
	return
}

func mockPod(name string) *api.Pod {
	return &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				mockOwnerReference(),
			},
		},
		Spec: api.PodSpec{
			Containers: []api.Container{
				mockContainer("twitter-cass-tweet", 0.25, 0.25, 256, 256),
				mockContainer("istio-proxy", 0.01, 2.0, 0, 8192),
				// Test that 0.5 mCore will be rounded up to 1 mCore
				mockContainer("filebeat-sidecar", 0.0005, 0.0005, 0, 0),
			},
		},
	}
}

func mockNode(name string, capacity, allocatable api.ResourceList) *api.Node {
	labels := make(map[string]string)
	labels["l1"] = "v1"
	labels["l2"] = "v2"
	node := &api.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "node",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Status: api.NodeStatus{
			Capacity:    capacity,
			Allocatable: allocatable,
		},
	}
	return node
}

func TestGenNodeResourceMetrics(t *testing.T) {
	// Build a node
	node := mockNode(
		"mynode",
		buildResource(2.0, 8192), // node capacity: 2.0 cores, 8 GiB mem
		buildResource(1.9, 7168), // node allocatable: 1.9 cores, 7 GiB mem
	)
	// Build pods in the node
	pods := []*api.Pod{mockPod("mypod")}
	config := &ClusterMonitorConfig{}
	clusterMonitor, err := NewClusterMonitor(config)
	if err != nil {
		t.Errorf("Failed to create clusterMonitor: %v", err)
	}
	clusterMonitor.clusterClient = cluster.NewClusterScraper(nil, &client.Clientset{}, nil, nil, nil, nil, "")
	clusterMonitor.sink = metrics.NewEntityMetricSink()
	clusterMonitor.node = node
	clusterMonitor.nodePodMap = make(map[string][]*api.Pod)
	clusterMonitor.nodePodMap["mynode"] = pods
	// Collect node/pod/container metrics
	_ = clusterMonitor.findNodeStates()
	for name, value := range expectedMetrics {
		metric, err := clusterMonitor.sink.GetMetric(name)
		if err != nil {
			t.Errorf("Failed to validate metric: %v", err)
		}
		assert.EqualValues(t, value, metric.GetValue(), fmt.Sprintf("Metric values are not equal for %s", name))
	}
}
