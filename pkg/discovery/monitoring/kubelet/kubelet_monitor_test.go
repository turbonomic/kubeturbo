package kubelet

import (
	"fmt"
	"math"
	"testing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	api "k8s.io/client-go/pkg/api/v1"
	stats "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
)

const (
	myzero = float64(0.00000001)
)

func createContainerStat(name string, cpu, mem int) stats.ContainerStats {
	usecores := uint64(cpu)
	usesecs := uint64(2105741823718)
	cpuInfo := &stats.CPUStats{
		UsageNanoCores:       &usecores,
		UsageCoreNanoSeconds: &usesecs,
	}

	usedbytes := uint64(mem)
	memoryInfo := &stats.MemoryStats{
		UsageBytes: &usedbytes,
	}

	container := stats.ContainerStats{
		Name:   name,
		CPU:    cpuInfo,
		Memory: memoryInfo,
	}

	return container
}

func createPodStat() *stats.PodStats {
	container1 := createContainerStat("container1", 5000000, 430*1000*1024)
	container2 := createContainerStat("container2", 6000000, 71*1000*1024)
	containers := []stats.ContainerStats{
		container1,
		container2,
	}
	pod := &stats.PodStats{
		PodRef: stats.PodReference{
			Namespace: "space1",
			Name:      "pod1",
			UID:       "uuid1",
		},
		Containers: containers,
	}

	return pod
}

func checkPodMetrics(sink *metrics.EntityMetricSink, podMId string, pod *stats.PodStats) error {
	etype := metrics.PodType
	resources := []metrics.ResourceType{metrics.CPU, metrics.Memory}

	for _, res := range resources {
		mid := metrics.GenerateEntityResourceMetricUID(etype, podMId, res, metrics.Used)
		tmp, err := sink.GetMetric(mid)
		if err != nil {
			return fmt.Errorf("Failed to get resource[%v] used value: %v", res, err)
		}

		value := tmp.GetValue().(float64)
		expected := float64(0.0)
		if res == metrics.CPU {
			for _, c := range pod.Containers {
				expected += float64(*c.CPU.UsageNanoCores)
			}
			expected = expected / util.NanoToUnit
		} else {
			for _, c := range pod.Containers {
				expected += float64(*c.Memory.UsageBytes)
			}
			expected = expected / util.KilobytesToBytes
		}

		if math.Abs(value-expected) > myzero {
			return fmt.Errorf("pod %v used value check failed: %v Vs. %v", res, expected, value)
		}
	}

	return nil
}

func checkContainerMetrics(sink *metrics.EntityMetricSink, containerMId string, container *stats.ContainerStats) error {
	etype := metrics.ContainerType
	resources := []metrics.ResourceType{metrics.CPU, metrics.Memory}

	for _, res := range resources {
		mid := metrics.GenerateEntityResourceMetricUID(etype, containerMId, res, metrics.Used)
		tmp, err := sink.GetMetric(mid)
		if err != nil {
			return fmt.Errorf("Failed to get resource[%v] used value: %v", res, err)
		}

		value := tmp.GetValue().(float64)
		expected := float64(0.0)
		if res == metrics.CPU {
			expected += float64(*container.CPU.UsageNanoCores)
			expected = expected / util.NanoToUnit
		} else {
			expected += float64(*container.Memory.UsageBytes)
			expected = expected / util.KilobytesToBytes
		}

		if math.Abs(value-expected) > myzero {
			return fmt.Errorf("container %v used value check failed: %v Vs. %v", res, expected, value)
		}
	}

	return nil
}

func checkApplicationMetrics(sink *metrics.EntityMetricSink, appMId string, container *stats.ContainerStats) error {
	etype := metrics.ApplicationType
	resources := []metrics.ResourceType{metrics.CPU, metrics.Memory}

	for _, res := range resources {
		mid := metrics.GenerateEntityResourceMetricUID(etype, appMId, res, metrics.Used)
		tmp, err := sink.GetMetric(mid)
		if err != nil {
			return fmt.Errorf("Failed to get resource[%v] used value: %v", res, err)
		}

		value := tmp.GetValue().(float64)
		expected := float64(0.0)
		if res == metrics.CPU {
			expected += float64(*container.CPU.UsageNanoCores)
			expected = expected / util.NanoToUnit
		} else {
			expected += float64(*container.Memory.UsageBytes)
			expected = expected / util.KilobytesToBytes
		}

		if math.Abs(value-expected) > myzero {
			return fmt.Errorf("Application %v used value check failed: %v Vs. %v", res, expected, value)
		}
	}

	return nil
}

func TestParseStats(t *testing.T) {
	conf := &KubeletMonitorConfig{}

	klet, err := NewKubeletMonitor(conf)
	if err != nil {
		t.Errorf("Failed to create kubeletMonitor: %v", err)
	}

	podstat := createPodStat()
	pods := []stats.PodStats{*podstat}
	klet.parsePodStats(pods)

	//1. check pod metrics
	podref := podstat.PodRef
	pod := &api.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: podref.Namespace,
			Name:      podref.Name,
			UID:       "pod.real.uuid1",
		},
	}
	podMId := util.PodMetricIdAPI(pod)
	err = checkPodMetrics(klet.metricSink, podMId, podstat)
	if err != nil {
		t.Errorf("check pod used metrics failed: %v", err)
		return
	}

	//2. container info
	for _, c := range podstat.Containers {
		containerMId := util.ContainerMetricId(podMId, c.Name)
		err = checkContainerMetrics(klet.metricSink, containerMId, &c)
		if err != nil {
			t.Errorf("check container used metrics failed: %v", err)
		}
	}

	//3. application info
	for _, c := range podstat.Containers {
		containerMId := util.ContainerMetricId(podMId, c.Name)
		appMId := util.ApplicationMetricId(containerMId)
		err = checkApplicationMetrics(klet.metricSink, appMId, &c)
		if err != nil {
			t.Errorf("check application used metrics failed: %v", err)
		}
	}
}
