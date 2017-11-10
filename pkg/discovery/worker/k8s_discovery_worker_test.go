package worker

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/kubelet"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	api "k8s.io/client-go/pkg/api/v1"
	"testing"
	"time"
)

func TestCalTimeOut(t *testing.T) {
	type Pair struct {
		nodeNum int
		timeout time.Duration
	}

	inputs := []*Pair{
		&Pair{
			nodeNum: 0,
			timeout: defaultMonitoringWorkerTimeout,
		},
		&Pair{
			nodeNum: 22,
			timeout: defaultMonitoringWorkerTimeout + time.Second*22,
		},
		&Pair{
			nodeNum: 500,
			timeout: defaultMonitoringWorkerTimeout + time.Second*500,
		},
		&Pair{
			nodeNum: 1500,
			timeout: defaultMaxTimeout,
		},
	}

	for _, input := range inputs {
		result := calcTimeOut(input.nodeNum)
		if result != input.timeout {
			t.Errorf("tiemout error: %v Vs. %v", result, input.timeout)
		}
	}
}

func TestBuildDTOsWithMissingMetrics(t *testing.T) {
	workerConfig := NewK8sDiscoveryWorkerConfig("UUID").WithMonitoringWorkerConfig(kubelet.NewKubeletMonitorConfig(nil))
	worker, err := NewK8sDiscoveryWorker(workerConfig, "wid-1")
	if err != nil {
		t.Errorf("Error while creating discovery worker: %v", err)
	}

	node := new(api.Node)
	node.UID = "uid-1"
	node.Name = "node-1"

	pod := new(api.Pod)
	pod.Name = "pod-1"

	currTask := task.NewTask().WithNodes([]*api.Node{node}).WithPods([]*api.Pod{pod})

	entities, err := worker.buildDTOs(currTask)
	if err != nil {
		t.Errorf("Error while building DTOs: %v", err)
	}

	if len(entities) != 0 {
		t.Errorf("Shouldn't build any entity DTO")
	}
}
