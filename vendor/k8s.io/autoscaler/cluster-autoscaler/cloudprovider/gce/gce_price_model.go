/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gce

import (
	"math"
	"strings"
	"time"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/autoscaler/cluster-autoscaler/utils/gpu"
	"k8s.io/autoscaler/cluster-autoscaler/utils/units"

	klog "k8s.io/klog/v2"
)

// GcePriceModel implements PriceModel interface for GCE.
type GcePriceModel struct {
}

const (
	//TODO: Move it to a config file.
	cpuPricePerHour         = 0.033174
	memoryPricePerHourPerGb = 0.004446
	preemptibleDiscount     = 0.00698 / 0.033174
	gpuPricePerHour         = 0.700

	preemptibleLabel = "cloud.google.com/gke-preemptible"
)

var (
	predefinedCpuPricePerHour = map[string]float64{
		"a2":  0.031611,
		"c2":  0.03398,
		"e2":  0.021811,
		"m1":  0.0348,
		"n1":  0.031611,
		"n2":  0.031611,
		"n2d": 0.027502,
	}
	predefinedMemoryPricePerHourPerGb = map[string]float64{
		"a2":  0.004237,
		"c2":  0.00455,
		"e2":  0.002923,
		"m1":  0.0051,
		"n1":  0.004237,
		"n2":  0.004237,
		"n2d": 0.003686,
	}
	predefinedPreemptibleDiscount = map[string]float64{
		"a2":  0.009483 / 0.031611,
		"c2":  0.00822 / 0.03398,
		"e2":  0.006543 / 0.021811,
		"m1":  0.00733 / 0.0348,
		"n1":  0.006655 / 0.031611,
		"n2":  0.007650 / 0.031611,
		"n2d": 0.006655 / 0.027502,
	}

	customCpuPricePerHour = map[string]float64{
		"e2":  0.022890,
		"n1":  0.033174,
		"n2":  0.033174,
		"n2d": 0.028877,
	}
	customMemoryPricePerHourPerGb = map[string]float64{
		"e2":  0.003067,
		"n1":  0.004446,
		"n2":  0.004446,
		"n2d": 0.003870,
	}
	customPreemptibleDiscount = map[string]float64{
		"e2":  0.006867 / 0.022890,
		"n1":  0.00698 / 0.033174,
		"n2":  0.00802 / 0.033174,
		"n2d": 0.006980 / 0.028877,
	}

	// e2-micro and e2-small have allocatable set too high resulting in
	// overcommit. To make cluster autoscaler prefer e2-medium given the choice
	// between the three machine types, the prices for e2-micro and e2-small
	// are artificially set to be higher than the price of e2-medium.
	instancePrices = map[string]float64{
		"a2-highgpu-1g":    3.673385,
		"a2-highgpu-2g":    7.34677,
		"a2-highgpu-4g":    14.69354,
		"a2-highgpu-8g":    29.38708,
		"a2-megagpu-16g":   55.739504,
		"c2-standard-4":    0.2088,
		"c2-standard-8":    0.4176,
		"c2-standard-16":   0.8352,
		"c2-standard-30":   1.5660,
		"c2-standard-60":   3.1321,
		"e2-highcpu-2":     0.04947,
		"e2-highcpu-4":     0.09894,
		"e2-highcpu-8":     0.19788,
		"e2-highcpu-16":    0.39576,
		"e2-highcpu-32":    0.79149,
		"e2-highmem-2":     0.09040,
		"e2-highmem-4":     0.18080,
		"e2-highmem-8":     0.36160,
		"e2-highmem-16":    0.72320,
		"e2-medium":        0.03351,
		"e2-micro":         0.03353, // Should be 0.00838. Set to be > e2-medium.
		"e2-small":         0.03352, // Should be 0.01675. Set to be > e2-medium.
		"e2-standard-2":    0.06701,
		"e2-standard-4":    0.13402,
		"e2-standard-8":    0.26805,
		"e2-standard-16":   0.53609,
		"e2-standard-32":   1.07210,
		"f1-micro":         0.0076,
		"g1-small":         0.0257,
		"m1-megamem-96":    10.6740,
		"m1-ultramem-40":   6.3039,
		"m1-ultramem-80":   12.6078,
		"m1-ultramem-160":  25.2156,
		"m2-ultramem-208":  42.186,
		"m2-ultramem-416":  84.371,
		"n1-highcpu-2":     0.0709,
		"n1-highcpu-4":     0.1418,
		"n1-highcpu-8":     0.2836,
		"n1-highcpu-16":    0.5672,
		"n1-highcpu-32":    1.1344,
		"n1-highcpu-64":    2.2688,
		"n1-highcpu-96":    3.402,
		"n1-highmem-2":     0.1184,
		"n1-highmem-4":     0.2368,
		"n1-highmem-8":     0.4736,
		"n1-highmem-16":    0.9472,
		"n1-highmem-32":    1.8944,
		"n1-highmem-64":    3.7888,
		"n1-highmem-96":    5.6832,
		"n1-standard-1":    0.0475,
		"n1-standard-2":    0.0950,
		"n1-standard-4":    0.1900,
		"n1-standard-8":    0.3800,
		"n1-standard-16":   0.7600,
		"n1-standard-32":   1.5200,
		"n1-standard-64":   3.0400,
		"n1-standard-96":   4.5600,
		"n2-highcpu-2":     0.0717,
		"n2-highcpu-4":     0.1434,
		"n2-highcpu-8":     0.2868,
		"n2-highcpu-16":    0.5736,
		"n2-highcpu-32":    1.1471,
		"n2-highcpu-48":    1.7207,
		"n2-highcpu-64":    2.2943,
		"n2-highcpu-80":    2.8678,
		"n2-highmem-2":     0.1310,
		"n2-highmem-4":     0.2620,
		"n2-highmem-8":     0.5241,
		"n2-highmem-16":    1.0481,
		"n2-highmem-32":    2.0962,
		"n2-highmem-48":    3.1443,
		"n2-highmem-64":    4.1924,
		"n2-highmem-80":    5.2406,
		"n2-standard-2":    0.0971,
		"n2-standard-4":    0.1942,
		"n2-standard-8":    0.3885,
		"n2-standard-16":   0.7769,
		"n2-standard-32":   1.5539,
		"n2-standard-48":   2.3308,
		"n2-standard-64":   3.1078,
		"n2-standard-80":   3.8847,
		"n2d-highcpu-2":    0.0624,
		"n2d-highcpu-4":    0.1248,
		"n2d-highcpu-8":    0.2495,
		"n2d-highcpu-16":   0.4990,
		"n2d-highcpu-32":   0.9980,
		"n2d-highcpu-48":   1.4970,
		"n2d-highcpu-64":   1.9960,
		"n2d-highcpu-80":   2.4950,
		"n2d-highcpu-96":   2.9940,
		"n2d-highcpu-128":  3.9920,
		"n2d-highcpu-224":  6.9861,
		"n2d-highmem-2":    0.1140,
		"n2d-highmem-4":    0.2280,
		"n2d-highmem-8":    0.4559,
		"n2d-highmem-16":   0.9119,
		"n2d-highmem-32":   1.8237,
		"n2d-highmem-48":   2.7356,
		"n2d-highmem-64":   3.6474,
		"n2d-highmem-80":   4.5593,
		"n2d-highmem-96":   5.4711,
		"n2d-standard-2":   0.0845,
		"n2d-standard-4":   0.1690,
		"n2d-standard-8":   0.3380,
		"n2d-standard-16":  0.6759,
		"n2d-standard-32":  1.3519,
		"n2d-standard-48":  2.0278,
		"n2d-standard-64":  2.7038,
		"n2d-standard-80":  3.3797,
		"n2d-standard-96":  4.0556,
		"n2d-standard-128": 5.4075,
		"n2d-standard-224": 9.4632,
	}
	preemptiblePrices = map[string]float64{
		"a2-highgpu-1g":    1.102016,
		"a2-highgpu-2g":    2.204031,
		"a2-highgpu-4g":    4.408062,
		"a2-highgpu-8g":    8.816124,
		"a2-megagpu-16g":   16.721851,
		"c2-standard-4":    0.0505,
		"c2-standard-8":    0.1011,
		"c2-standard-16":   0.2021,
		"c2-standard-30":   0.3790,
		"c2-standard-60":   0.7579,
		"e2-highcpu-2":     0.01484,
		"e2-highcpu-4":     0.02968,
		"e2-highcpu-8":     0.05936,
		"e2-highcpu-16":    0.11873,
		"e2-highcpu-32":    0.23744,
		"e2-highmem-2":     0.02712,
		"e2-highmem-4":     0.05424,
		"e2-highmem-8":     0.10848,
		"e2-highmem-16":    0.21696,
		"e2-medium":        0.01005,
		"e2-micro":         0.01007, // Should be 0.00251. Set to be > e2-medium.
		"e2-small":         0.01006, // Should be 0.00503. Set to be > e2-medium.
		"e2-standard-2":    0.02010,
		"e2-standard-4":    0.04021,
		"e2-standard-8":    0.08041,
		"e2-standard-16":   0.16083,
		"e2-standard-32":   0.32163,
		"f1-micro":         0.0035,
		"g1-small":         0.0070,
		"m1-megamem-96":    2.2600,
		"m1-ultramem-40":   1.3311,
		"m1-ultramem-80":   2.6622,
		"m1-ultramem-160":  5.3244,
		"n1-highcpu-2":     0.0150,
		"n1-highcpu-4":     0.0300,
		"n1-highcpu-8":     0.0600,
		"n1-highcpu-16":    0.1200,
		"n1-highcpu-32":    0.2400,
		"n1-highcpu-64":    0.4800,
		"n1-highcpu-96":    0.7200,
		"n1-highmem-2":     0.0250,
		"n1-highmem-4":     0.0500,
		"n1-highmem-8":     0.1000,
		"n1-highmem-16":    0.2000,
		"n1-highmem-32":    0.4000,
		"n1-highmem-64":    0.8000,
		"n1-highmem-96":    1.2000,
		"n1-standard-1":    0.0100,
		"n1-standard-2":    0.0200,
		"n1-standard-4":    0.0400,
		"n1-standard-8":    0.0800,
		"n1-standard-16":   0.1600,
		"n1-standard-32":   0.3200,
		"n1-standard-64":   0.6400,
		"n1-standard-96":   0.9600,
		"n2-highcpu-2":     0.0173,
		"n2-highcpu-4":     0.0347,
		"n2-highcpu-8":     0.0694,
		"n2-highcpu-16":    0.1388,
		"n2-highcpu-32":    0.2776,
		"n2-highcpu-48":    0.4164,
		"n2-highcpu-64":    0.5552,
		"n2-highcpu-80":    0.6940,
		"n2-highmem-2":     0.0317,
		"n2-highmem-4":     0.0634,
		"n2-highmem-8":     0.1268,
		"n2-highmem-16":    0.2536,
		"n2-highmem-32":    0.5073,
		"n2-highmem-48":    0.7609,
		"n2-highmem-64":    1.0145,
		"n2-highmem-80":    1.2681,
		"n2-standard-2":    0.0235,
		"n2-standard-4":    0.0470,
		"n2-standard-8":    0.0940,
		"n2-standard-16":   0.1880,
		"n2-standard-32":   0.3760,
		"n2-standard-48":   0.5640,
		"n2-standard-64":   0.7520,
		"n2-standard-80":   0.9400,
		"n2d-highcpu-2":    0.0151,
		"n2d-highcpu-4":    0.0302,
		"n2d-highcpu-8":    0.0604,
		"n2d-highcpu-16":   0.1208,
		"n2d-highcpu-32":   0.2415,
		"n2d-highcpu-48":   0.3623,
		"n2d-highcpu-64":   0.4830,
		"n2d-highcpu-80":   0.6038,
		"n2d-highcpu-96":   0.7245,
		"n2d-highcpu-128":  0.9660,
		"n2d-highcpu-224":  1.6905,
		"n2d-highmem-2":    0.0276,
		"n2d-highmem-4":    0.0552,
		"n2d-highmem-8":    0.1103,
		"n2d-highmem-16":   0.2207,
		"n2d-highmem-32":   0.4413,
		"n2d-highmem-48":   0.6620,
		"n2d-highmem-64":   0.8826,
		"n2d-highmem-80":   1.1033,
		"n2d-highmem-96":   1.3239,
		"n2d-standard-2":   0.0204,
		"n2d-standard-4":   0.0409,
		"n2d-standard-8":   0.0818,
		"n2d-standard-16":  0.1636,
		"n2d-standard-32":  0.3271,
		"n2d-standard-48":  0.4907,
		"n2d-standard-64":  0.6543,
		"n2d-standard-80":  0.8178,
		"n2d-standard-96":  0.9814,
		"n2d-standard-128": 1.3085,
		"n2d-standard-224": 2.2900,
	}
	gpuPrices = map[string]float64{
		"nvidia-tesla-t4":   0.35,
		"nvidia-tesla-p4":   0.60,
		"nvidia-tesla-v100": 2.48,
		"nvidia-tesla-p100": 1.46,
		"nvidia-tesla-k80":  0.45,
		"nvidia-tesla-a100": 0, // price of this gpu is counted into A2 machine-type price
	}
	preemptibleGpuPrices = map[string]float64{
		"nvidia-tesla-t4":   0.11,
		"nvidia-tesla-p4":   0.216,
		"nvidia-tesla-v100": 0.74,
		"nvidia-tesla-p100": 0.43,
		"nvidia-tesla-k80":  0.135,
		"nvidia-tesla-a100": 0, // price of this gpu is counted into A2 machine-type price
	}
)

// NodePrice returns a price of running the given node for a given period of time.
// All prices are in USD.
func (model *GcePriceModel) NodePrice(node *apiv1.Node, startTime time.Time, endTime time.Time) (float64, error) {
	price := 0.0
	basePriceFound := false
	isPreemptible := false

	// Base instance price
	if node.Labels != nil {
		isPreemptible = node.Labels[preemptibleLabel] == "true"
		if machineType, found := node.Labels[apiv1.LabelInstanceType]; found {
			priceMapToUse := instancePrices
			if isPreemptible {
				priceMapToUse = preemptiblePrices
			}
			if basePricePerHour, found := priceMapToUse[machineType]; found {
				price = basePricePerHour * getHours(startTime, endTime)
				basePriceFound = true
			} else {
				klog.Warningf("Pricing information not found for instance type %v; will fallback to default pricing", machineType)
			}
		}
	}
	if !basePriceFound {
		price = getBasePrice(node.Status.Capacity, node.Labels[apiv1.LabelInstanceType], startTime, endTime)
		price = price * getPreemptibleDiscount(node)
	}

	// GPUs
	if gpuRequest, found := node.Status.Capacity[gpu.ResourceNvidiaGPU]; found {
		gpuPrice := gpuPricePerHour
		if node.Labels != nil {
			priceMapToUse := gpuPrices
			if isPreemptible {
				priceMapToUse = preemptibleGpuPrices
			}
			if gpuType, found := node.Labels[GPULabel]; found {
				if _, found := priceMapToUse[gpuType]; found {
					gpuPrice = priceMapToUse[gpuType]
				} else {
					klog.Warningf("Pricing information not found for GPU type %v; will fallback to default pricing", gpuType)
				}
			}
		}
		price += float64(gpuRequest.MilliValue()) / 1000.0 * gpuPrice * getHours(startTime, endTime)
	}

	// TODO: handle SSDs.
	return price, nil
}

func getHours(startTime time.Time, endTime time.Time) float64 {
	minutes := math.Ceil(float64(endTime.Sub(startTime)) / float64(time.Minute))
	hours := minutes / 60.0
	return hours
}

func getInstanceFamily(instanceType string) string {
	return strings.Split(instanceType, "-")[0]
}

func isInstanceCustom(instanceType string) bool {
	return strings.Contains(instanceType, "custom")
}

func getPreemptibleDiscount(node *apiv1.Node) float64 {
	if node.Labels[preemptibleLabel] != "true" {
		return 1.0
	}
	instanceType := node.Labels[apiv1.LabelInstanceType]
	instanceFamily := getInstanceFamily(instanceType)

	discountMap := predefinedPreemptibleDiscount
	if isInstanceCustom(instanceType) {
		discountMap = customPreemptibleDiscount
	}

	if _, found := discountMap[instanceFamily]; found {
		return discountMap[instanceFamily]
	}
	return preemptibleDiscount
}

// PodPrice returns a theoretical minimum price of running a pod for a given
// period of time on a perfectly matching machine.
func (model *GcePriceModel) PodPrice(pod *apiv1.Pod, startTime time.Time, endTime time.Time) (float64, error) {
	price := 0.0
	for _, container := range pod.Spec.Containers {
		price += getBasePrice(container.Resources.Requests, "", startTime, endTime)
		price += getAdditionalPrice(container.Resources.Requests, startTime, endTime)
	}
	return price, nil
}

func getBasePrice(resources apiv1.ResourceList, instanceType string, startTime time.Time, endTime time.Time) float64 {
	if len(resources) == 0 {
		return 0
	}
	hours := getHours(startTime, endTime)
	instanceFamily := getInstanceFamily(instanceType)
	isCustom := isInstanceCustom(instanceType)
	price := 0.0

	cpu := resources[apiv1.ResourceCPU]
	cpuPrice := cpuPricePerHour
	cpuPriceMap := predefinedCpuPricePerHour
	if isCustom {
		cpuPriceMap = customCpuPricePerHour
	}
	if _, found := cpuPriceMap[instanceFamily]; found {
		cpuPrice = cpuPriceMap[instanceFamily]
	}
	price += float64(cpu.MilliValue()) / 1000.0 * cpuPrice * hours

	mem := resources[apiv1.ResourceMemory]
	memPrice := memoryPricePerHourPerGb
	memPriceMap := predefinedMemoryPricePerHourPerGb
	if isCustom {
		memPriceMap = customMemoryPricePerHourPerGb
	}
	if _, found := memPriceMap[instanceFamily]; found {
		memPrice = memPriceMap[instanceFamily]
	}
	price += float64(mem.Value()) / float64(units.GiB) * memPrice * hours

	return price
}

func getAdditionalPrice(resources apiv1.ResourceList, startTime time.Time, endTime time.Time) float64 {
	if len(resources) == 0 {
		return 0
	}
	hours := getHours(startTime, endTime)
	price := 0.0
	gpu := resources[gpu.ResourceNvidiaGPU]
	price += float64(gpu.MilliValue()) / 1000.0 * gpuPricePerHour * hours
	return price
}
