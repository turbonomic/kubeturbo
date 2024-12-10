package custom

import (
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
	k8sapi "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

func GeneratePodSpecForCustomController(kind string, obj map[string]interface{}, podSpec *k8sapi.PodSpec) (bool, error) {
	switch kind {
	case util.KindVirtualMachine:
		return updatePodSpecForKubevirtVirtualMachine(obj, podSpec)
	}

	return false, nil
}

// CheckAndUpdateCustomController general entry for all custom resources, check them one by one
func CheckAndUpdateCustomController(kind string, obj *unstructured.Unstructured, desiredPodSpec *k8sapi.PodSpec) (*unstructured.Unstructured, bool, error) {
	var err error

	switch kind {
	case util.KindVirtualMachine:
		updatedObj, updated, err := checkAndUpdateKubevirtVirtualMachine(obj, desiredPodSpec)
		return updatedObj, updated, err
	}

	return obj, false, err
}

// CompleteCustomController general entry for all custom resources, check them one by one
func CompleteCustomController(cli dynamic.ResourceInterface, kind string, obj *unstructured.Unstructured) error {
	var err error

	switch kind {
	case util.KindVirtualMachine:
		err = completeKubevirtVirtualMachine(cli, obj)
		return err
	}

	return nil
}
