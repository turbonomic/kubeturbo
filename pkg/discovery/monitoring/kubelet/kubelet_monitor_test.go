package kubelet

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"testing"

	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	stats "k8s.io/kubelet/pkg/apis/stats/v1alpha1"

	"github.com/stretchr/testify/assert"
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/kubeclient"
)

const (
	myzero    = float64(0.00000001)
	timestamp = 1
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
		WorkingSetBytes: &usedbytes,
	}

	container := stats.ContainerStats{
		Name:   name,
		CPU:    cpuInfo,
		Memory: memoryInfo,
	}

	return container
}

func createPodStat(podname string, containerStatsMissing bool) *stats.PodStats {
	cpuUsed := 100 + rand.Intn(200)
	memUsed := rand.Intn(200)
	container1 := createContainerStat("container1", cpuUsed*1e6, memUsed*1000*1024)
	cpuUsed = rand.Intn(100)
	memUsed = 200 + rand.Intn(200)
	container2 := createContainerStat("container2", cpuUsed*1e6, memUsed*1000*1024)
	containers := []stats.ContainerStats{
		container1,
		container2,
	}
	pod := &stats.PodStats{
		PodRef: stats.PodReference{
			Namespace: "space1",
			Name:      podname,
			UID:       "uuid1",
		},
	}
	if !containerStatsMissing {
		pod.Containers = containers
	}

	fsStats := &stats.FsStats{}
	capacity := uint64(rand.Intn(200) * 1e9)
	used := uint64(rand.Intn(100) * 1e9)
	fsStats.CapacityBytes = &capacity
	fsStats.UsedBytes = &used
	pod.EphemeralStorage = fsStats

	return pod
}

func checkContainerMetricsAvailability(sink *metrics.EntityMetricSink, pod *api.Pod, isMissing bool) error {
	isAvailable := true
	entityKey := util.PodMetricIdAPI(pod)
	ownerMetricId := metrics.GenerateEntityStateMetricUID(metrics.PodType, entityKey, metrics.MetricsAvailability)
	availabilityMetric, err := sink.GetMetric(ownerMetricId)
	if err != nil {
		return fmt.Errorf("error getting %s from metrics sink for pod %s --> %v", metrics.MetricsAvailability, entityKey, err)
	} else {
		availabilityMetricValue := availabilityMetric.GetValue()
		ok := false
		isAvailable, ok = availabilityMetricValue.(bool)
		if !ok {
			return fmt.Errorf("error getting %s from metrics sink for pod %s. Wrong type: %T, Expected: bool.", metrics.MetricsAvailability, entityKey, availabilityMetricValue)
		}
	}

	// We have isAvailable set in sink which should be opposite of isMissing
	if isMissing == isAvailable {
		return fmt.Errorf("failed matching container metric availability, got: %t, expected: %t.", isAvailable, isMissing)
	}
	return nil
}

func checkPodMetrics(sink *metrics.EntityMetricSink, podMId string, pod *stats.PodStats) error {
	etype := metrics.PodType
	resources := []metrics.ResourceType{metrics.CPU, metrics.Memory}

	for _, res := range resources {
		mid := metrics.GenerateEntityResourceMetricUID(etype, podMId, res, metrics.Used)
		tmp, err := sink.GetMetric(mid)
		if err != nil {
			return fmt.Errorf("failed to get resource[%v] used value: %v", res, err)
		}

		valuePoints := tmp.GetValue().([]metrics.Point)
		expected := float64(0.0)
		if res == metrics.CPU {
			for _, c := range pod.Containers {
				expected += float64(*c.CPU.UsageNanoCores)
			}
			expected = util.MetricNanoToMilli(expected)
		} else {
			for _, c := range pod.Containers {
				expected += float64(*c.Memory.WorkingSetBytes)
			}
			expected = util.Base2BytesToKilobytes(expected)
		}

		if math.Abs(valuePoints[0].Value-expected) > myzero {
			return fmt.Errorf("pod %v used value check failed: %v Vs. %v", res, expected, valuePoints[0].Value)
		}
		if timestamp != valuePoints[0].Timestamp {
			return fmt.Errorf("pod %v metric timestamp check failed: %v Vs. %v", res, timestamp, valuePoints[0].Timestamp)
		}

		//fmt.Printf("%v, v=%.4f Vs. %.4f\n", mid, value, expected)
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
			return fmt.Errorf("failed to get resource[%v] used value: %v", res, err)
		}

		valuePoints := tmp.GetValue().([]metrics.Point)
		expected := float64(0.0)
		if res == metrics.CPU {
			expected += float64(*container.CPU.UsageNanoCores)
			expected = util.MetricNanoToMilli(expected)
		} else {
			expected += float64(*container.Memory.WorkingSetBytes)
			expected = util.Base2BytesToKilobytes(expected)
		}

		if math.Abs(valuePoints[0].Value-expected) > myzero {
			return fmt.Errorf("container %v used value check failed: %v Vs. %v", res, expected, valuePoints[0].Value)
		}
		if timestamp != valuePoints[0].Timestamp {
			return fmt.Errorf("container %v metric timestamp value check failed: %v Vs. %v", res, timestamp, valuePoints[0].Timestamp)
		}
		//fmt.Printf("%v, v=%.4f Vs. %.4f\n", mid, value, expected)
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
			return fmt.Errorf("failed to get resource[%v] used value: %v", res, err)
		}

		valuePoints := tmp.GetValue().([]metrics.Point)
		expected := float64(0.0)
		if res == metrics.CPU {
			expected += float64(*container.CPU.UsageNanoCores)
			expected = util.MetricNanoToMilli(expected)
		} else {
			expected += float64(*container.Memory.WorkingSetBytes)
			expected = util.Base2BytesToKilobytes(expected)
		}

		if math.Abs(valuePoints[0].Value-expected) > myzero {
			return fmt.Errorf("application %v used value check failed: %v Vs. %v", res, expected, valuePoints[0].Value)
		}
		if timestamp != valuePoints[0].Timestamp {
			return fmt.Errorf("application %v metric timestamp check failed: %v Vs. %v", res, timestamp, valuePoints[0].Timestamp)
		}
		//fmt.Printf("%v, v=%.4f Vs. %.4f\n", mid, value, expected)
	}

	return nil
}

func TestParseStats(t *testing.T) {
	conf := &KubeletMonitorConfig{}

	klet, err := NewKubeletMonitor(conf, true)
	if err != nil {
		t.Errorf("Failed to create kubeletMonitor: %v", err)
	}

	podTests := []struct {
		podStat               *stats.PodStats
		containerStatsMissing bool
	}{
		{
			podStat:               createPodStat("pod1", false),
			containerStatsMissing: false,
		},
		{
			podStat:               createPodStat("pod2", false),
			containerStatsMissing: false,
		},
		{
			podStat:               createPodStat("pod3", true),
			containerStatsMissing: true,
		},
	}

	podStats := []stats.PodStats{}
	for _, podTest := range podTests {
		podStats = append(podStats, *podTest.podStat)
	}
	klet.parsePodStats(podStats, timestamp)

	for _, podTest := range podTests {
		podref := podTest.podStat.PodRef
		pod := &api.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: podref.Namespace,
				Name:      podref.Name,
				UID:       "pod.real.uuid1",
			},
		}

		//1. check container metrics availability
		err = checkContainerMetricsAvailability(klet.metricSink, pod, podTest.containerStatsMissing)
		if err != nil {
			t.Errorf("Check container metrics availability failed: %v", err)
			return
		}

		if podTest.containerStatsMissing {
			// There won't be other metrics to verify
			return
		}

		//2. check pod metrics
		podMId := util.PodMetricIdAPI(pod)
		err = checkPodMetrics(klet.metricSink, podMId, podTest.podStat)
		if err != nil {
			t.Errorf("Check pod used metrics failed: %v", err)
			return
		}

		//3. container info
		for _, c := range podTest.podStat.Containers {
			containerMId := util.ContainerMetricId(podMId, c.Name)
			err = checkContainerMetrics(klet.metricSink, containerMId, &c)
			if err != nil {
				t.Errorf("Check container used metrics failed: %v", err)
			}
		}

		//4. application info
		for _, c := range podTest.podStat.Containers {
			containerMId := util.ContainerMetricId(podMId, c.Name)
			appMId := util.ApplicationMetricId(containerMId)
			err = checkApplicationMetrics(klet.metricSink, appMId, &c)
			if err != nil {
				t.Errorf("Check application used metrics failed: %v", err)
			}
		}
	}
}

func TestGenThrottlingMetrics(t *testing.T) {
	kubeletMonitorConf := &KubeletMonitorConfig{}
	kubeletMonitor, _ := NewKubeletMonitor(kubeletMonitorConf, true)

	throttlingMetric := &throttlingMetric{
		cpuThrottled: 2,
		cpuTotal:     10,
		cpuQuota:     2000,
		cpuPeriod:    10000,
	}
	metricID := "cpu_throttling_metric"
	kubeletMonitor.genThrottlingMetrics(metrics.ContainerType, metricID, throttlingMetric, timestamp)
	key := metrics.GenerateEntityResourceMetricUID(metrics.ContainerType, metricID, metrics.VCPUThrottling, metrics.Used)
	metric, _ := kubeletMonitor.metricSink.GetMetric(key)
	throttlingMetricValue, ok := metric.GetValue().([]metrics.ThrottlingCumulative)

	assert.True(t, ok)
	expectedThrottlingMetricValue := []metrics.ThrottlingCumulative{{
		Throttled: 2,
		Total:     10,
		CPULimits: 200,
		Timestamp: timestamp,
	}}
	if !reflect.DeepEqual(expectedThrottlingMetricValue, throttlingMetricValue) {
		t.Errorf("genThrottlingMetrics() expected: %v, actual: %v", expectedThrottlingMetricValue, throttlingMetricValue)
	}
}

func TestParseMetricFamilies(t *testing.T) {
	// This sample is an actual metric copied from a live cluster to also document
	// how an actual metric retrieved from kubelet would look like.
	// The throttled period and total period values are updated to the test values.
	metricSample := []byte(`
# HELP container_cpu_cfs_periods_total Number of elapsed enforcement period intervals.
# TYPE container_cpu_cfs_periods_total counter
container_cpu_cfs_periods_total{container="",id="/kubepods/burstable/pod278c96f7-c22a-466a-85ae-69f221705a38",image="",name="",namespace="lens-metrics",pod="node-exporter-pmngv"} 476995 1616975911629
container_cpu_cfs_periods_total{container="",id="/kubepods/pod8266a379-dd56-42f8-8af0-19fc0d8ea3af",image="",name="",namespace="ccp",pod="metallb-speaker-m29mf"} 2.074517e+06 1616975909256
container_cpu_cfs_periods_total{container="metallb-speaker",id="/kubepods/pod8266a379-dd56-42f8-8af0-19fc0d8ea3af/8e1a2ff0f116c9d086af53cbd7430dced0e70fed104aea79e3891870564aed38",image="sha256:8c49f7de2c13b87026d7afb04f35494e5d9ce6b5eeeb7f8983d38e601d0ac910",name="k8s_metallb-speaker_metallb-speaker-m29mf_ccp_8266a379-dd56-42f8-8af0-19fc0d8ea3af_16",namespace="ccp",pod="metallb-speaker-m29mf"} 10 1616975907597
container_cpu_cfs_periods_total{container="node-exporter",id="/kubepods/burstable/pod278c96f7-c22a-466a-85ae-69f221705a38/6a79a7d41fcfb3347875e6bfa17a7c6daa7911ff42a0177906a2005b1e8ffa12",image="sha256:0e0218889c33b5fbb9e158d45ff6193c7c145b4ce3ec348045626cfa09f8331d",name="k8s_node-exporter_node-exporter-pmngv_lens-metrics_278c96f7-c22a-466a-85ae-69f221705a38_1",namespace="lens-metrics",pod="node-exporter-pmngv"} 20 1616975915711
# HELP container_cpu_cfs_throttled_periods_total Number of throttled period intervals.
# TYPE container_cpu_cfs_throttled_periods_total counter
container_cpu_cfs_throttled_periods_total{container="",id="/kubepods/burstable/pod278c96f7-c22a-466a-85ae-69f221705a38",image="",name="",namespace="lens-metrics",pod="node-exporter-pmngv"} 158736 1616976193974
container_cpu_cfs_throttled_periods_total{container="",id="/kubepods/pod8266a379-dd56-42f8-8af0-19fc0d8ea3af",image="",name="",namespace="ccp",pod="metallb-speaker-m29mf"} 38909 1616976189755
container_cpu_cfs_throttled_periods_total{container="metallb-speaker",id="/kubepods/pod8266a379-dd56-42f8-8af0-19fc0d8ea3af/8e1a2ff0f116c9d086af53cbd7430dced0e70fed104aea79e3891870564aed38",image="sha256:8c49f7de2c13b87026d7afb04f35494e5d9ce6b5eeeb7f8983d38e601d0ac910",name="k8s_metallb-speaker_metallb-speaker-m29mf_ccp_8266a379-dd56-42f8-8af0-19fc0d8ea3af_16",namespace="ccp",pod="metallb-speaker-m29mf"} 5 1616976197080
container_cpu_cfs_throttled_periods_total{container="node-exporter",id="/kubepods/burstable/pod278c96f7-c22a-466a-85ae-69f221705a38/6a79a7d41fcfb3347875e6bfa17a7c6daa7911ff42a0177906a2005b1e8ffa12",image="sha256:0e0218889c33b5fbb9e158d45ff6193c7c145b4ce3ec348045626cfa09f8331d",name="k8s_node-exporter_node-exporter-pmngv_lens-metrics_278c96f7-c22a-466a-85ae-69f221705a38_1",namespace="lens-metrics",pod="node-exporter-pmngv"} 15 1616976196348
# HELP container_spec_cpu_quota CPU quota of the container.
# TYPE container_spec_cpu_quota gauge
container_spec_cpu_quota{container="",id="/kubepods/burstable/pod278c96f7-c22a-466a-85ae-69f221705a38",image="",name="",namespace="lens-metrics",pod="node-exporter-pmngv"} 20000 1616975920718
container_spec_cpu_quota{container="",id="/kubepods/pod8266a379-dd56-42f8-8af0-19fc0d8ea3af",image="",name="",namespace="ccp",pod="metallb-speaker-m29mf"} 10000 1616976289754
container_spec_cpu_quota{container="metallb-speaker",id="/kubepods/pod8266a379-dd56-42f8-8af0-19fc0d8ea3af/8e1a2ff0f116c9d086af53cbd7430dced0e70fed104aea79e3891870564aed38",image="sha256:8c49f7de2c13b87026d7afb04f35494e5d9ce6b5eeeb7f8983d38e601d0ac910",name="k8s_metallb-speaker_metallb-speaker-m29mf_ccp_8266a379-dd56-42f8-8af0-19fc0d8ea3af_16",namespace="ccp",pod="metallb-speaker-m29mf"} 10000 1629775344665
container_spec_cpu_quota{container="node-exporter",id="/kubepods/burstable/pod278c96f7-c22a-466a-85ae-69f221705a38/6a79a7d41fcfb3347875e6bfa17a7c6daa7911ff42a0177906a2005b1e8ffa12",image="sha256:0e0218889c33b5fbb9e158d45ff6193c7c145b4ce3ec348045626cfa09f8331d",name="k8s_node-exporter_node-exporter-pmngv_lens-metrics_278c96f7-c22a-466a-85ae-69f221705a38_1",namespace="lens-metrics",pod="node-exporter-pmngv"} 20000 1616975915711
# HELP container_spec_cpu_period CPU period of the container.
# TYPE container_spec_cpu_period gauge
container_spec_cpu_period{container="",id="/kubepods/burstable/pod278c96f7-c22a-466a-85ae-69f221705a38",image="",name="",namespace="lens-metrics",pod="node-exporter-pmngv"} 100000 1616975920718
container_spec_cpu_period{container="",id="/kubepods/pod8266a379-dd56-42f8-8af0-19fc0d8ea3af",image="",name="",namespace="ccp",pod="metallb-speaker-m29mf"} 100000 1616976289754
container_spec_cpu_period{container="metallb-speaker",id="/kubepods/pod8266a379-dd56-42f8-8af0-19fc0d8ea3af/8e1a2ff0f116c9d086af53cbd7430dced0e70fed104aea79e3891870564aed38",image="sha256:8c49f7de2c13b87026d7afb04f35494e5d9ce6b5eeeb7f8983d38e601d0ac910",name="k8s_metallb-speaker_metallb-speaker-m29mf_ccp_8266a379-dd56-42f8-8af0-19fc0d8ea3af_16",namespace="ccp",pod="metallb-speaker-m29mf"} 100000 1629775404902
container_spec_cpu_period{container="node-exporter",id="/kubepods/burstable/pod278c96f7-c22a-466a-85ae-69f221705a38/6a79a7d41fcfb3347875e6bfa17a7c6daa7911ff42a0177906a2005b1e8ffa12",image="sha256:0e0218889c33b5fbb9e158d45ff6193c7c145b4ce3ec348045626cfa09f8331d",name="k8s_node-exporter_node-exporter-pmngv_lens-metrics_278c96f7-c22a-466a-85ae-69f221705a38_1",namespace="lens-metrics",pod="node-exporter-pmngv"} 100000 1616975915711
`)
	mfs, err := kubeclient.TextToThrottlingMetricFamilies(metricSample)
	if err != nil {
		t.Errorf("Unexpeced error parsing metric families: %v", err)
	}

	parsed := parseMetricFamilies(mfs)
	verifyParsedMetrics(t, parsed)
}

func verifyParsedMetrics(t *testing.T, got map[string]*throttlingMetric) {
	// The parsed metric is a map which should have keys as container ids formatted as
	// <namespace>/<podname>/<containername>
	// the namespace, podname and containername is extracted from the labels on
	// individual metric.
	// The throttled value and the total value is merged into:
	// type throttlingMetric struct {
	// cpuThrottled float64
	// cpuTotal     float64
	// cpuQuota     float64
	// cpuPeriod    float64
	// }
	// from the two separately reported metrics for each container.
	// The parsing ignores the metrics with container names = "" or "POD"
	// These metrics are sum total of all containers for a pod.
	expected := map[string]*throttlingMetric{
		"ccp/metallb-speaker-m29mf/metallb-speaker": {
			cpuThrottled: 5,
			cpuTotal:     10,
			cpuQuota:     10000,
			cpuPeriod:    100000,
		},
		"lens-metrics/node-exporter-pmngv/node-exporter": {
			cpuThrottled: 15,
			cpuTotal:     20,
			cpuQuota:     20000,
			cpuPeriod:    100000,
		},
	}

	// Returned throttlingMetrics only include 2 metrics
	assert.Equal(t, 2, len(got))

	for key, expectedMetrics := range expected {
		gotMetrics, exists := got[key]
		if !exists {
			t.Errorf("Missing metrics after parsing for container key: %s", key)
		}
		if !matchMetrics(gotMetrics, expectedMetrics) {
			t.Errorf("Parsed metrics don't match for: %s: got: %v++, expected: %v++", key, gotMetrics, expectedMetrics)
		}
	}
}

func matchMetrics(got, expected *throttlingMetric) bool {
	return almostEqual(got.cpuThrottled, expected.cpuThrottled) && almostEqual(got.cpuTotal, expected.cpuTotal)
}

const float64EqualityThreshold = 1e-9

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) <= float64EqualityThreshold
}
