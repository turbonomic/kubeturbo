package executor

import (
	"fmt"
	"testing"

	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type mockMachineSetController struct {
	*machineSetController
}

func (controller *mockMachineSetController) getMaxReplicas() int {
	return 10
}

func (controller *mockMachineSetController) getMinReplicas() int {
	return 4
}

func (controller *mockMachineSetController) checkMachineSet(_ ...interface{}) (bool, error) {
	return true, nil
}

func makeIntPointer(i int) *int32 {
	v := int32(i)
	return &v
}

func mockMachinesList(numberOfMachines int) *machinev1beta1.MachineList {
	machineTypeMeta := metav1.TypeMeta{
		Kind:       "Machine",
		APIVersion: "machine.openshift.io/v1beta1",
	}
	machines := make([]machinev1beta1.Machine, numberOfMachines)
	for i := 0; i < int(numberOfMachines); i++ {
		machines[i] = machinev1beta1.Machine{
			TypeMeta: machineTypeMeta,
			ObjectMeta: metav1.ObjectMeta{
				Name:              fmt.Sprintf("machine%d", i+1),
				DeletionTimestamp: nil,
			},
		}
	}

	return &machinev1beta1.MachineList{
		Items: machines,
	}
}

func Test_machineSetController_checkPreconditions(t *testing.T) {
	type fields struct {
		request        *actionRequest
		machineSet     *machinev1beta1.MachineSet
		machinesInfo   []*machineInfo
		machineList    *machinev1beta1.MachineList
		controllerName string
		namespace      string
	}
	tests := []struct {
		name         string
		fields       fields
		expectedDiff int
		wantErr      bool
	}{
		{
			name: "test scale down from 5 to 2 becomes sale from 5 to 4",
			fields: fields{
				request:    &actionRequest{nil, -3, SuspendAction},
				machineSet: &machinev1beta1.MachineSet{Spec: machinev1beta1.MachineSetSpec{Replicas: makeIntPointer(5)}},
				machinesInfo: []*machineInfo{
					{actionItemId: int64(111), machine: nil, actionResult: "Success"},
				},
				machineList:    mockMachinesList(5),
				controllerName: "",
				namespace:      "",
			},
			expectedDiff: -1,
			wantErr:      false,
		},
		{
			name: "test scale down from 4 to 1 becomes error",
			fields: fields{
				request:    &actionRequest{nil, -3, SuspendAction},
				machineSet: &machinev1beta1.MachineSet{Spec: machinev1beta1.MachineSetSpec{Replicas: makeIntPointer(4)}},
				machinesInfo: []*machineInfo{
					{actionItemId: int64(111), machine: nil, actionResult: "Success"},
				},
				machineList:    mockMachinesList(4),
				controllerName: "",
				namespace:      "",
			},
			expectedDiff: -3,
			wantErr:      true,
		},
		{
			name: "test scale up from 2 to 3 acceptable",
			fields: fields{
				request:    &actionRequest{nil, 1, ProvisionAction},
				machineSet: &machinev1beta1.MachineSet{Spec: machinev1beta1.MachineSetSpec{Replicas: makeIntPointer(2)}},
				machinesInfo: []*machineInfo{
					{actionItemId: int64(111), machine: nil, actionResult: "Success"},
				},
				machineList:    mockMachinesList(2),
				controllerName: "",
				namespace:      "",
			},
			expectedDiff: 1,
			wantErr:      false,
		},
		{
			name: "test scale up from 10 to 11 error",
			fields: fields{
				request:    &actionRequest{nil, 1, ProvisionAction},
				machineSet: &machinev1beta1.MachineSet{Spec: machinev1beta1.MachineSetSpec{Replicas: makeIntPointer(10)}},
				machinesInfo: []*machineInfo{
					{actionItemId: int64(111), machine: nil, actionResult: "Success"},
				},
				machineList:    mockMachinesList(10),
				controllerName: "",
				namespace:      "",
			},
			expectedDiff: 1,
			wantErr:      true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &mockMachineSetController{}
			// currentReplicas := int(*controller.machineSet.Spec.Replicas)
			// diff := int(controller.request.diff)
			// controller.machinesInfo

			controller.machineSetController = &machineSetController{
				request:                 tt.fields.request,
				machineSet:              tt.fields.machineSet,
				machinesInfo:            tt.fields.machinesInfo,
				machineList:             tt.fields.machineList,
				controllerName:          tt.fields.controllerName,
				namespace:               tt.fields.namespace,
				controllerUtilityHelper: controller, // point to the mock controller to use mock functions
			}
			err := controller.checkPreconditions()

			if (err != nil) != tt.wantErr {
				t.Errorf("machineSetController.checkPreconditions() error = %v, wantErr %v", err, tt.wantErr)
			}
			if outputDiff := controller.request.diff; outputDiff != int32(tt.expectedDiff) {
				t.Errorf("Expected diff %d, got %d", tt.expectedDiff, outputDiff)
			}
		})
	}
}
