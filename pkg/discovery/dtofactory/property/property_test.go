package property

import (
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
	"strconv"

	"github.com/stretchr/testify/assert"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"testing"
)

const (
	clusterKey = "clusterX"

	label1Key   = "label1"
	label1Value = "value1"
	label2Key   = "label2"
	label2Value = "value2"

	taintAKey    = "taintA"
	taintAEffect = api.TaintEffectNoSchedule
	taintBKey    = "taintB"
	taintBValue  = "foo"
	taintBEffect = api.TaintEffectNoExecute
	taintCEffect = api.TaintEffectPreferNoSchedule

	toleration1Key    = taintAKey
	toleration1Op     = api.TolerationOpExists
	toleration1Effect = taintAEffect
	toleration2Key    = taintBKey
	toleration2Op     = api.TolerationOpEqual
	toleration2Value  = taintBValue
	toleration2Effect = taintBEffect
	toleration3Effect = taintCEffect
)

func TestNodeProperty(t *testing.T) {
	labels := make(map[string]string)
	labels[label1Key] = label1Value
	labels[label2Key] = label2Value

	node := &api.Node{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Node",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:   "my-node-1",
			UID:    "my-node-1-UID",
			Labels: labels,
		},

		Spec: api.NodeSpec{
			Taints: []api.Taint{
				{
					Key:    taintAKey,
					Effect: taintAEffect,
				},
				{
					Key:    taintBKey,
					Value:  taintBValue,
					Effect: taintBEffect,
				},
				{
					Effect: taintCEffect,
				},
			},
		},
	}

	ps := BuildNodeProperties(node, clusterKey)
	nodeName, err := GetNodeNameFromProperties(ps)
	assert.Nil(t, err)
	assert.Equal(t, node.Name, nodeName, "Node name retrieved from entity properties does not match with the expected")

	fqn, err := GetFullyQualifiedNameFromProperties(ps)
	assert.Nil(t, err)
	assert.Equal(t, clusterKey+util.NamingQualifierSeparator+node.Name, fqn,
		"FQN retrieved from entity properties does not match with the expected")

	matches := 0
	for _, p := range ps {
		if p.GetNamespace() == VCTagsPropertyNamespace {
			var expected string
			switch p.GetName() {
			case LabelPropertyNamePrefix + " " + label1Key:
				expected = label1Value
			case LabelPropertyNamePrefix + " " + label2Key:
				expected = label2Value
			case TaintPropertyNamePrefix + " " + string(taintAEffect):
				expected = taintAKey
			case TaintPropertyNamePrefix + " " + string(taintBEffect):
				expected = taintBKey + "=" + taintBValue
			case TaintPropertyNamePrefix + " " + string(taintCEffect):
				expected = ""
			default:
				continue
			}
			matches++
			assert.EqualValues(t, expected, p.GetValue())
		}
	}
	assert.Equal(t, 5, matches, "there should be 5 matches in the test node properties")
}

func TestBuildPodProperties(t *testing.T) {
	labels := make(map[string]string)
	labels[label1Key] = label1Value
	labels[label2Key] = label2Value

	pod := &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod1",
			Namespace: "my-namespace",
			UID:       "my-pod-1-UID",
			Labels:    labels,
		},
		Spec: api.PodSpec{
			Tolerations: []api.Toleration{
				{
					Key:      toleration1Key,
					Operator: toleration1Op,
					Effect:   toleration1Effect,
				},
				{
					Key:      toleration2Key,
					Operator: toleration2Op,
					Value:    toleration2Value,
					Effect:   toleration2Effect,
				},
				{
					Effect: toleration3Effect,
				},
			},
		},
	}

	ps := BuildPodProperties(pod, clusterKey)
	ns, name, err := GetPodInfoFromProperty(ps)
	if err != nil {
		t.Errorf("pod property test failed: %v", err)
		return
	}

	if ns != pod.Namespace {
		t.Errorf("Pod property test failed: namespace is wrong (%v) Vs. (%v)", ns, pod.Namespace)
	}

	if name != pod.Name {
		t.Errorf("Pod property test failed: pod name is wrong: (%v) Vs. (%v)", name, pod.Name)
	}

	matches := 0
	for _, p := range ps {
		if p.GetNamespace() == VCTagsPropertyNamespace {
			var expected string
			switch p.GetName() {
			case LabelPropertyNamePrefix + " " + label1Key:
				expected = label1Value
			case LabelPropertyNamePrefix + " " + label2Key:
				expected = label2Value
			case TolerationPropertyNamePrefix + " " + string(toleration1Effect):
				expected = toleration1Key + " " + string(toleration1Op)
			case TolerationPropertyNamePrefix + " " + string(toleration2Effect):
				expected = toleration2Key + "=" + toleration2Value
			case TolerationPropertyNamePrefix + " " + string(toleration3Effect):
				expected = ""
			default:
				continue
			}
			matches++
			assert.EqualValues(t, expected, p.GetValue())
		}
	}
	assert.Equal(t, 5, matches, "there should be 5 matches in the test pod properties")
}

func TestBuildPodPropertiesWithSeconds(t *testing.T) {
	tolerationSeconds := int64(300)
	pod := &api.Pod{
		Spec: api.PodSpec{
			Tolerations: []api.Toleration{
				{
					Key:               "key",
					Operator:          "op",
					Effect:            "effect",
					TolerationSeconds: &tolerationSeconds,
				},
			},
		},
	}

	ps := BuildPodProperties(pod, clusterKey)

	for _, p := range ps {
		if p.GetName() == "[k8s toleration] effect" {
			assert.EqualValues(t, "key op for 300s", p.GetValue())
			return
		}
	}

	assert.Fail(t, "tolerationSeconds is not found.")
}

func TestAddContainerProperties(t *testing.T) {
	clusterId := "123"
	namespace := "xyz"
	podName := "poda"
	containerName := "containerb"
	index := 2

	ps := AddContainerProperties(clusterId, namespace, podName, containerName, index)
	ns, pn, cn, idx := GetContainerProperties(ps)

	if ns != namespace {
		t.Error("Application perperty test failed: namespace is wrong")
	}

	if pn != podName {
		t.Error("Application property test failed: pod name is wrong")
	}

	if cn != containerName {
		t.Error("Application property test failed: container name is wrong")
	}

	if idx != index {
		t.Error("Appliction property test failed: container index is wrong.")
	}
}

func TestAddVolumeProperties(t *testing.T) {
	vls := []api.Volume{
		{
			Name: "my-pvc-1",
			VolumeSource: api.VolumeSource{
				PersistentVolumeClaim: &api.PersistentVolumeClaimVolumeSource{
					ClaimName: "my-pvc-1",
				},
			},
		},
		{
			Name: "my-pvc-2",
			VolumeSource: api.VolumeSource{
				PersistentVolumeClaim: &api.PersistentVolumeClaimVolumeSource{
					ClaimName: "my-pvc-2",
				},
			},
		},
	}
	pod := &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod1",
			Namespace: "my-namespace",
			UID:       "my-pod-1-UID",
		},
		Spec: api.PodSpec{
			Volumes: vls,
		},
	}

	ps := BuildPodProperties(pod, clusterKey)
	ps = AddVolumeProperties(ps)
	ns, name, err := GetPodInfoFromProperty(ps)
	if err != nil {
		t.Errorf("Add volume property test failed: %v", err)
		return
	}

	if ns != pod.Namespace {
		t.Errorf("Add volume property test failed: namespace is wrong (%v) Vs. (%v)", ns, pod.Namespace)
	}

	if name != pod.Name {
		t.Errorf("Add volume property test: pod name is wrong: (%v) Vs. (%v)", name, pod.Name)
	}
	expected := strconv.FormatBool(true)
	for _, p := range ps {
		if p.GetNamespace() == VCTagsPropertyNamespace && p.GetName() == k8sVolumeAttached {
			assert.EqualValues(t, expected, p.GetValue())
			return
		}
	}
	assert.Fail(t, "Can't find volume property in the pod's properties")
}

func TestAddVirtualMachineInstanceProperties(t *testing.T) {
	pod := &api.Pod{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},

		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-pod1",
			Namespace: "my-namespace",
			UID:       "my-pod-1-UID",
		},
	}

	ps := BuildPodProperties(pod, clusterKey)
	ps = AddVirtualMachineInstanceProperties(ps, "true")
	ns, name, err := GetPodInfoFromProperty(ps)
	if err != nil {
		t.Errorf("Add VirtualMachineInstance property test failed: %v", err)
		return
	}

	if ns != pod.Namespace {
		t.Errorf("Add VirtualMachineInstance property test failed: namespace is wrong (%v) Vs. (%v)", ns, pod.Namespace)
	}

	if name != pod.Name {
		t.Errorf("Add VirtualMachineInstance property test: pod name is wrong: (%v) Vs. (%v)", name, pod.Name)
	}
	expected := strconv.FormatBool(true)
	for _, p := range ps {
		if p.GetNamespace() == VCTagsPropertyNamespace && p.GetName() == IsVirtualMachineInstance {
			assert.EqualValues(t, expected, p.GetValue())
			return
		}
	}
	assert.Fail(t, "Can't find VirtualMachineInstance property in the pod's properties")
}
