package actionrepo

import (
	"reflect"
	"testing"

	"github.com/vmturbo/kubeturbo/pkg/action/turboaction"
)

func TestAdd(t *testing.T) {
	actionRepo := NewActionRepository()
	actionEventBuilder := turboaction.NewVMTEventBuilder("")
	targetObjectName1 := "Foo"
	targetObjectType1 := "Pod"
	actionContent1 := turboaction.TurboActionContent{
		TargetObject: turboaction.TargetObject{targetObjectName1, targetObjectType1},
	}
	action1 := actionEventBuilder.Content(actionContent1).Create()

	actionRepo.Add(&action1)
	actions := actionRepo.actions
	if actionMap, exist := actions[targetObjectType1]; !exist {
		t.Errorf("Cannot find expected actions with object type %s in reposity.", targetObjectType1)
	} else {
		if len(actionMap) != 1 {
			t.Errorf("Size of actoinMap is incorrect, expected 1, got %d", len(actionMap))
		}

		if actionList, found := actionMap[targetObjectName1]; !found {
			t.Errorf("Cannot find expected Actions with object name %s in reposity.", targetObjectName1)
		} else {
			if len(actionList) != 1 {
				t.Errorf("Length of actionList is incorrect, expected 1, got %d", len(actionList))
			}
			for _, action := range actionList {
				if reflect.DeepEqual(action, &action1) {
					return
				}
			}
			t.Error("Cannot find expect action in action repository.")
		}
	}
}

func TestDelete(t *testing.T) {
	actionRepo := NewActionRepository()
	actionEventBuilder := turboaction.NewVMTEventBuilder("")
	targetObjectName1 := "Foo"
	targetObjectType1 := "Pod"
	actionContent1 := turboaction.TurboActionContent{
		TargetObject: turboaction.TargetObject{targetObjectName1, targetObjectType1},
	}
	action1 := actionEventBuilder.Content(actionContent1).Create()

	actionRepo.Add(&action1)
	err := actionRepo.Delete(&action1)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	actions := actionRepo.actions
	if _, exist := actions[targetObjectType1]; exist {
		t.Errorf("Action with object type %s is found. But it is expected to be deleted from repository.: %+v", targetObjectType1, actionRepo)
	}
}

func TestFindActionInList(t *testing.T) {
	actionEventBuilder := turboaction.NewVMTEventBuilder("")
	targetObjectName1 := "Foo"
	targetObjectType1 := "Pod"
	actionContent1 := turboaction.TurboActionContent{
		TargetObject: turboaction.TargetObject{targetObjectName1, targetObjectType1},
	}
	action1 := actionEventBuilder.Content(actionContent1).Create()

	actionList := []*turboaction.TurboAction{&action1}
	index, err := findActionInList(actionList, &action1)
	if err != nil {
		t.Errorf("Unexpected error: %s", err)
	}
	if index != 0 {
		t.Errorf("Expected find action at index %d, got %d", 0, index)
	}
}

func TestGetEntityTypeAndName(t *testing.T) {
	table := []struct {
		targetObjectName   string
		targetObjectType   string
		parentObjectName   string
		parentObjectType   string
		expectedEntityName string
		expectedEntityType string
	}{
		{
			"Foo",
			"Pod",
			"",
			"",
			"Foo",
			"Pod",
		},
		{
			"Foo",
			"Pod",
			"Foo_RC",
			"ReplicationController",
			"Foo_RC",
			"ReplicationController",
		},
	}
	for _, item := range table {
		actionEventBuilder := turboaction.NewVMTEventBuilder("")
		actionContent := turboaction.TurboActionContent{
			TargetObject: turboaction.TargetObject{item.targetObjectName, item.targetObjectType},
		}
		if item.parentObjectName != "" && item.parentObjectType != "" {
			actionContent.ParentObjectRef = turboaction.ParentObjectRef{
				item.parentObjectName,
				item.parentObjectType,
			}
		}
		action := actionEventBuilder.Content(actionContent).Create()
		eType, eName := getEntityTypeAndName(&action)
		if eType != item.expectedEntityType || eName != item.expectedEntityName {
			t.Errorf("Expected %s with name %s, got %s with name %s", item.expectedEntityType,
				item.expectedEntityName, eType, eName)
		}
	}

}
