package executor

import (
	"context"
	"fmt"
	"github.ibm.com/turbonomic/kubeturbo/pkg/cluster"
	helper "github.ibm.com/turbonomic/kubeturbo/pkg/discovery/worker/compliance/podaffinity/testing"
	commonutil "github.ibm.com/turbonomic/kubeturbo/pkg/util"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
	"time"
)

// moveVMIPod moves the VirtualMachineInstance pod by following the OpenShift Virtualization API.
func moveVMIPod(clusterScraper *cluster.ClusterScraper, pod *api.Pod, nodeName, parentKind string) (*api.Pod, error) {
	//0. Ensure this pod is controlled by the VirtualMachine kind
	_, gParent, _, nsGpClient, gpKind, err := getPodOwnersInfo(clusterScraper, pod, parentKind)
	if err != nil {
		return nil, err
	}
	if gParent == nil || gpKind != commonutil.KindVirtualMachine {
		return nil, fmt.Errorf("the controller of pod %s is not a virtual machine; move isn't supported", pod.GetName())
	}

	//0. Remove the affinity annotation in a deferred manner
	defer func() {
		err = RemoveNodeAffinity(nsGpClient, gParent, nodeName)
	}()

	//1. Annotate the OpenShift VirtualMachine with the affinity rule to specify the destination node
	if err = appendNodeAffinity(nsGpClient, gParent, nodeName); err != nil {
		return nil, err
	}

	//2. Perform the live migration and wait for it to succeed
	if err = performLiveMigration(clusterScraper.DynamicClient, gParent); err != nil {
		return nil, err
	}

	return nil, err
}

// appendNodeAffinity adds the node affinity expression for the VirtualMachine.
// Node affinity specification is a list of lists.  Each inner list is a list of ANDed conditions, while the outer list
// is an ORed of all inner lists.  Therefore, when we append the node affinity expression, we will add it to each inner
// list.
func appendNodeAffinity(client dynamic.ResourceInterface, vm *unstructured.Unstructured, nodeName string) error {
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		objCopy, err := client.Get(context.TODO(), vm.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}

		addLabel(objCopy, TurboMoveLabelKey, nodeName)
		affinityAsMap, found, err := unstructured.NestedMap(objCopy.Object, "spec", "template", "spec", "affinity")
		if err != nil {
			return err
		}

		var affinity api.Affinity
		if found {
			if err = runtime.DefaultUnstructuredConverter.FromUnstructured(affinityAsMap, &affinity); err != nil {
				return err
			}
		}

		updatedAffinity := helper.MakeAffinityWrapper(&affinity).AddSingleNodeAffinityExpression(nodeName).Obj()

		newAffinityAsMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(updatedAffinity)
		if err != nil {
			return err
		}
		if err := unstructured.SetNestedMap(objCopy.Object, newAffinityAsMap, "spec", "template", "spec", "affinity"); err != nil {
			return err
		}

		_, err = client.Update(context.TODO(), objCopy, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		return nil
	})

	if retryErr != nil {
		return fmt.Errorf("error setting node affinity to drive VM %s move to %s: %v", vm.GetName(), nodeName, retryErr)
	}
	return nil
}

// RemoveNodeAffinity removes the previously appended node affinity rule for the VirtualMachine.
// Node affinity specification is a list of lists.  Each inner list is a list of ANDed conditions, while the outer list
// is an ORed of all inner lists.  Therefore, when we remove the node affinity expression, we will remove it from each
// the inner list.
func RemoveNodeAffinity(client dynamic.ResourceInterface, vm *unstructured.Unstructured, nodeName string) error {
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		objCopy, err := client.Get(context.TODO(), vm.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}

		affinityMap, found, err := unstructured.NestedMap(objCopy.Object, "spec", "template", "spec", "affinity")
		if err != nil {
			return err
		}
		if !found {
			// return success
			return nil
		}

		var affinity api.Affinity
		if err = runtime.DefaultUnstructuredConverter.FromUnstructured(affinityMap, &affinity); err != nil {
			return err
		}

		updatedAffinity := helper.MakeAffinityWrapper(&affinity).RemoveSingleNodeAffinityExpression(nodeName).Obj()
		if updatedAffinity == nil {
			unstructured.RemoveNestedField(objCopy.Object, "spec", "template", "spec", "affinity")
		} else {
			newAffinityMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(updatedAffinity)
			if err != nil {
				return err
			}
			if err := unstructured.SetNestedMap(objCopy.Object, newAffinityMap, "spec", "template", "spec", "affinity"); err != nil {
				return err
			}
		}
		removeLabel(objCopy, TurboMoveLabelKey)

		_, err = client.Update(context.TODO(), objCopy, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		return nil
	})

	if retryErr != nil {
		return fmt.Errorf("error removing node affinity while trying to move VM %s to %s: %v", vm.GetName(), nodeName, retryErr)
	}
	return nil
}

// performLiveMigration initiates a live migration for the given VM.
func performLiveMigration(client dynamic.Interface, vm *unstructured.Unstructured) error {
	vmimName := vm.GetName() + "-migration-" + rand.String(5)
	vmim := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": commonutil.OpenShiftVMIMGV.String(),
			"kind":       commonutil.KindVirtualMachineInstanceMigration,
			"metadata": map[string]interface{}{
				"name": vmimName,
			},
			"spec": map[string]interface{}{
				"vmiName": vm.GetName(),
			},
		},
	}
	vmimRes := schema.GroupVersionResource{
		Group:    commonutil.OpenShiftVMIMGV.Group,
		Version:  commonutil.OpenShiftVMIMGV.Version,
		Resource: commonutil.VirtualMachineInstanceMigrationResName,
	}

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := client.Resource(vmimRes).Namespace(vm.GetNamespace()).Create(context.TODO(), vmim, metav1.CreateOptions{})
		return err
	})
	if retryErr != nil {
		return fmt.Errorf("error initiating a live migration for VM %s: %v", vm.GetName(), retryErr)
	}

	// Wait for the live migration to succeed or time out to cancel the live migration
	retryErr = retry.OnError(
		wait.Backoff{
			Steps:    5,
			Duration: 10 * time.Second,
			Factor:   2.0,
			Jitter:   0.1,
		},
		func(error) bool {
			return true
		},
		func() error {
			result, err := client.Resource(vmimRes).Namespace(vm.GetNamespace()).Get(context.TODO(), vmimName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("error locating the VMI migration CR %s in namespace %s: %v", vmim.GetName(), vm.GetNamespace(), err)
			}
			value, found, err := unstructured.NestedBool(result.Object, "status", "migrationState", "completed")
			if err != nil {
				return fmt.Errorf("error retrieving the migration status for %s from result %s: %v", vmim.GetName(), result.Object, err)
			}
			if found == false || value == false {
				// migration yet to complete... wait
				return fmt.Errorf("the migration for %s has not completed yet", vmim.GetName())
			}
			return nil
		},
	)

	if retryErr != nil {
		// cancel the migration as we have problems verifying it
		if err := client.Resource(vmimRes).Namespace(vm.GetNamespace()).Delete(context.TODO(), vmim.GetName(), metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("error canceling a live migration for VM %s after the completion of the migration cannot be confirmed: %v; canceling error: %s", vm.GetName(), retryErr, err)
		}
		return fmt.Errorf("the completion of migration of %s cannot be confirmed: %v", vmim.GetName(), retryErr)
	}

	return nil
}
