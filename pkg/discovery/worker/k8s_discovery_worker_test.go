package worker

import (
	"reflect"
	"testing"
	"time"

	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/kubelet"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	workerConfig := NewK8sDiscoveryWorkerConfig("UUID").WithMonitoringWorkerConfig(kubelet.NewKubeletMonitorConfig(nil, nil))
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

	_, _, _, err = worker.buildDTOs(currTask)
	if err != nil {
		t.Errorf("Error while building DTOs: %v", err)
	}
}

func TestExcludeFailedPods(t *testing.T) {
	p1 := newPod("p1", "id-1")
	p2 := newPod("p2", "id-2")
	p3 := newPod("p3", "id-3")

	d1 := newEntityDTO("id-1")
	d2 := newEntityDTO("id-2")
	d3 := newEntityDTO("id-3")
	pods := []*api.Pod{p1, p2, p3}

	tests := []struct {
		name string
		pods []*api.Pod
		dtos []*proto.EntityDTO
		want []*api.Pod
	}{
		{
			name: "test-empty",
			pods: []*api.Pod{},
			dtos: []*proto.EntityDTO{},
			want: []*api.Pod{},
		},
		{
			name: "test-all-passed",
			pods: pods,
			dtos: []*proto.EntityDTO{d1, d2, d3},
			want: pods,
		},
		{
			name: "test-all-failed",
			pods: pods,
			dtos: []*proto.EntityDTO{},
			want: []*api.Pod{},
		},
		{
			name: "test-p1-failed",
			pods: pods,
			dtos: []*proto.EntityDTO{d2, d3},
			want: []*api.Pod{p2, p3},
		},
		{
			name: "test-p1-p3-failed",
			pods: pods,
			dtos: []*proto.EntityDTO{d2},
			want: []*api.Pod{p2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := excludeFailedPods(tt.pods, tt.dtos); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Test %s: excludeFailedPods() = %++v, want %++v", tt.name, got, tt.want)
			}
		})
	}
}

func newPod(name string, uid types.UID) *api.Pod {
	return &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			UID:  uid,
		},

		Spec: api.PodSpec{},
	}
}

func newEntityDTO(id string) *proto.EntityDTO {
	return &proto.EntityDTO{
		Id: &id,
	}
}
