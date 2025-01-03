package custom

import (
	"context"
	"errors"
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
	"time"

	k8sapi "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
)

func updatePodSpecForKubevirtVirtualMachine(obj map[string]interface{}, podSpec *k8sapi.PodSpec) (bool, error) {

	if podSpec == nil {
		return false, errors.New("nil PodSpec passed in")
	}

	unstructuredResReq, found, err := unstructured.NestedFieldCopy(obj, "spec", "template", "spec", "domain", "resources")
	if err != nil || !found {
		return found, err
	}

	resReq := k8sapi.ResourceRequirements{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredResReq.(map[string]interface{}), &resReq); err != nil {
		return false, err
	}

	container := k8sapi.Container{
		Name:      util.ComputeContainerName,
		Resources: resReq,
	}
	podSpec.Containers = []k8sapi.Container{
		container,
	}

	return true, nil
}

func checkAndUpdateKubevirtVirtualMachine(obj *unstructured.Unstructured, podSpec *k8sapi.PodSpec) (*unstructured.Unstructured, bool, error) {

	if podSpec == nil || obj == nil {
		return obj, false, errors.New("nil PodSpec or Object passed in")
	}

	err := unstructured.SetNestedField(obj.Object, false, "spec", "running")
	if err != nil {
		return obj, false, err
	}

	resUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&(podSpec.Containers[0].Resources))
	if err != nil {
		return obj, false, err
	}

	err = unstructured.SetNestedField(obj.Object, resUnstructured, "spec", "template", "spec", "domain", "resources")
	if err != nil {
		return obj, false, err
	}

	return obj, true, nil
}

func completeKubevirtVirtualMachine(cli dynamic.ResourceInterface, obj *unstructured.Unstructured) error {

	var err error
	var updatedObj *unstructured.Unstructured

	for retry := 5; retry > 0; retry-- {
		updatedObj, err = cli.Get(context.TODO(), obj.GetName(), metav1.GetOptions{})
		if err != nil {
			break
		}

		err = unstructured.SetNestedField(updatedObj.Object, true, "spec", "running")
		if err != nil {
			continue
		}

		_, err = cli.Update(context.TODO(), updatedObj, metav1.UpdateOptions{})
		if err == nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	return err
}
