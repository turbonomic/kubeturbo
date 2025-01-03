package gitops

import (
	"encoding/json"
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"

	argodiff "github.com/argoproj/gitops-engine/pkg/diff"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sapiyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

const (
	KubeturboNamespaceEnv = "KUBETURBO_NAMESPACE"
	CommitModeRequest     = "request"
	CommitModeDirect      = "direct"
)

func DecodeAndMatchName(fileData, name string) (bool, error) {
	obj, err := DecodeYaml(fileData)
	if err != nil {
		return false, err
	}
	if obj.GetName() == name {
		return true, nil
	}

	return false, nil
}

// We find the diff between the resource in the source of truth and the liveRes using
// gitops-engine libs which is sort of a wrapper around the k8s libs which implement
// kubectl diff.
//
// This enables us to not fall into the traps of unnecessary diff resulting
// because of differing units but same values and/or extra defaulted fields
// in the original (live) resource. This also takes care of differently ordered keys
// in the specs (effectively comparing the actual resources).
//
// We however still use the json patch mechanism to finally patch the source of truth yaml and
// write it back in the repo.
func PatchYamlContent(configYaml string, replicas int64, livePodSpec map[string]interface{}) (bool, []byte, error) {
	configRes, err := DecodeYaml(configYaml)
	if err != nil {
		return false, nil, err
	}

	configPodSpec, found, err := unstructured.NestedFieldCopy(configRes.Object, "spec", "template", "spec")
	if err != nil || !found {
		return false, nil, fmt.Errorf("error retrieving podSpec from gitops config resource %s %s: %v",
			configRes.GetKind(), configRes.GetName(), err)
	}
	configPodReplicas, found, err := unstructured.NestedFieldCopy(configRes.Object, "spec", "replicas")
	if err != nil || !found {
		return false, nil, fmt.Errorf("error retrieving replicas from gitops config resource %s %s: %v",
			configRes.GetKind(), configRes.GetName(), err)
	}

	// use copies to ensure our podspecs do not change, yet
	configPod := podWithSpec(configPodSpec)
	livePod := podWithSpec(livePodSpec).DeepCopy()

	// We compare the podSpecs by setting them into a pod/v1 resource because a gvk is expected
	// by the diff lib for comparison to correctly decipher the k8s resource defaulted fields.
	// We could as well have compared the live resource (eg the deployement from which the pod
	// spec is coming) but that results in unwanted diffs because of additional labels or
	// annotations sometimes set locally on the live resource. We should ignore them.
	diffResult, err := argodiff.Diff(configPod, livePod, argodiff.WithNormalizer(argodiff.GetNoopNormalizer()))
	if err != nil {
		return false, nil, err
	}

	modified := diffResult.Modified || configPodReplicas.(int64) != replicas
	var patchedConfigYaml []byte
	if modified {
		// We also remove all the unwanted additional defaulted fields from the live spec
		// so as not to update that back into the source of truth yaml
		livePodSpec = removeMapFields(configPodSpec.(map[string]interface{}), livePodSpec)
		patchedConfigYaml, err = applyPatch([]byte(configYaml), getPatchItems(replicas, livePodSpec))
		if err != nil {
			return false, nil, err
		}
	}

	return modified, patchedConfigYaml, nil
}

func podWithSpec(podSpec interface{}) *unstructured.Unstructured {
	pod := &unstructured.Unstructured{}
	pod.Object = map[string]interface{}{
		"spec": podSpec,
	}
	pod.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Kind:    "Pod",
		Version: "v1",
	})

	return pod
}

func DecodeYaml(yaml string) (*unstructured.Unstructured, error) {
	obj := &unstructured.Unstructured{}
	decoder := k8sapiyaml.NewYAMLToJSONDecoder(strings.NewReader(yaml))
	err := decoder.Decode(obj)
	if err != nil {
		return nil, err
	}

	return obj, nil
}

// copied from https://github.com/argoproj/gitops-engine/blob/v0.7.0/pkg/utils/json/json.go
// but updated to remove all differing fields from live res unlike the original code
// RemoveMapFields remove all non-existent fields in the live that don't exist in the config
func removeMapFields(config, live map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{}
	for k, v1 := range config {
		v2, ok := live[k]
		if !ok {
			continue
		}
		if v2 != nil {
			v2 = removeFields(v1, v2)
		}
		result[k] = v2
	}
	return result
}

func removeFields(config, live interface{}) interface{} {
	if config == nil {
		return nil
	}
	switch c := config.(type) {
	case map[string]interface{}:
		l, ok := live.(map[string]interface{})
		if ok {
			return removeMapFields(c, l)
		} else {
			return live
		}
	case []interface{}:
		l, ok := live.([]interface{})
		if ok {
			return removeListFields(c, l)
		} else {
			return live
		}
	default:
		return live
	}
}

func removeListFields(config, live []interface{}) []interface{} {
	result := make([]interface{}, 0, len(live))
	for i, v2 := range live {
		if len(config) > i {
			if v2 != nil {
				v2 = removeFields(config[i], v2)
			}
			result = append(result, v2)
		}
		// We skip removing additional list items which exist in live but not
		// in config this assumes that the lists will be ordered
	}
	return result
}

func getPatchItems(replicas int64, podSpec map[string]interface{}) []PatchItem {
	return []PatchItem{
		{
			Op:    "replace",
			Path:  "/spec/replicas",
			Value: replicas,
		},
		{
			Op:   "replace",
			Path: "/spec/template/spec",
			// TODO: update only specific fields of each container in the pod
			// rather then the whole pod spec
			Value: podSpec,
		},
	}
}

func applyPatch(yamlBytes []byte, patches []PatchItem) ([]byte, error) {
	jsonPatchBytes, err := json.Marshal(patches)
	if err != nil {
		return nil, err
	}

	patch, err := jsonpatch.DecodePatch(jsonPatchBytes)
	if err != nil {
		return nil, err
	}

	jsonBytes, err := yaml.YAMLToJSON(yamlBytes)
	if err != nil {
		return nil, err
	}

	patchedJsonBytes, err := patch.Apply(jsonBytes)
	if err != nil {
		return nil, err
	}

	return yaml.JSONToYAML(patchedJsonBytes)
}
