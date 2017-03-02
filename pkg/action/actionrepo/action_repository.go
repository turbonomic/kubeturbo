package actionrepo

import (
	"fmt"
	"sync"

	"github.com/vmturbo/kubeturbo/pkg/action/turboaction"
	"errors"
)

// ActionRepository stores all the actions in PENDING state.
// NOTE: right now only move and provision action will be put here.
type ActionRepository struct {
	// Actions are stored according to their parent object type and name.
	// For example, ["ReplicationController"]["frontend"][event1, event2,...].
	// If there is no parentObjectRef, use the type and name of targetObject.
	actions map[string]map[string][]*turboaction.TurboAction

	lock sync.RWMutex
}

func NewActionRepository() *ActionRepository {
	return &ActionRepository{
		actions: make(map[string]map[string][]*turboaction.TurboAction),
	}
}

// Add an action event to repository.
func (r *ActionRepository) Add(action *turboaction.TurboAction) {
	r.lock.Lock()
	defer r.lock.Unlock()

	entityType, entityName := getEntityTypeAndName(action)

	var actionMap map[string][]*turboaction.TurboAction
	if m, exist := r.actions[entityType]; exist {
		actionMap = m
	} else {
		actionMap = make(map[string][]*turboaction.TurboAction)
		r.actions[entityType] = actionMap
	}
	var actionList []*turboaction.TurboAction
	if l, exist := actionMap[entityName]; exist {
		actionList = l
	}
	actionList = append(actionList, action)

	actionMap[entityName] = actionList
	r.actions[entityType] = actionMap
}

// Delete the given action event from repository.
func (r *ActionRepository) Delete(action *turboaction.TurboAction) error {
	r.lock.Lock()
	defer r.lock.Unlock()

	entityType, entityName := getEntityTypeAndName(action)

	actionMap, exist := r.actions[entityType]
	if !exist {
		return errors.New("Error deleting action. Cannot find the action in repository.")
	}
	aList, exist := actionMap[entityName]
	if !exist {
		return errors.New("Error deleting action. Cannot find the action in repository.")
	}
	if i, err := findActionInList(aList, action); err == nil {
		// delete record in slice.
		aList = append(aList[:i], aList[i+1:]...)
	} else {
		return fmt.Errorf("Error deleting action. Cannot find action in action list: %s", err)
	}
	if len(aList) == 0 {
		// Nothing else in the list. Delete the enty.
		delete(actionMap, entityName)
		if len(actionMap) == 0 {
			delete(r.actions, entityType)
		}
	} else {
		actionMap[entityName] = aList
		r.actions[entityType] = actionMap
	}
	return nil
}

// Find action based on its UID, and return the index in the slice.
func findActionInList(actionList []*turboaction.TurboAction, action *turboaction.TurboAction) (int, error) {
	for i, a := range actionList {
		if a.UID == action.UID {
			return i, nil
		}
	}
	return -1, errors.New("Cannot find action record in repository")
}

// Given entity type and name, find the corresponding action event.
// TODO if there is no action related to given type and name, do we want to return error?
func (r *ActionRepository) GetAction(eType, eName string) (*turboaction.TurboAction, error) {
	r.lock.RLock()
	defer r.lock.RUnlock()

	actionMap, exist := r.actions[eType]
	if !exist {
		return nil, fmt.Errorf("Cannot find any action based on entity type %s.", eType)
	}
	actionList, exist := actionMap[eName]
	if !exist {
		return nil, fmt.Errorf("Cannot find any action based on entity type %s and entity name %s.", eType, eName)
	}
	for _, a := range actionList {
		//	if a.Content.ActionType == actionType {
		return a, nil
		//      }
	}
	return nil, fmt.Errorf("Cannot find any action based on entity type %s and entity name %s.", eType, eName)

}

// Given an action event, return the related action type and name.
// If the action event has a parentObjectRef, then use the info from there;
// otherwise use the info from targetObject.
func getEntityTypeAndName(action *turboaction.TurboAction) (string, string) {
	content := action.Content
	var entityType, entityName string
	if action.Content.ParentObjectRef != (turboaction.ParentObjectRef{}) {
		entityType = content.ParentObjectRef.ParentObjectType
		entityName = content.ParentObjectRef.ParentObjectName
	} else {
		entityType = content.TargetObject.TargetObjectType
		entityName = content.TargetObject.TargetObjectName
	}
	return entityType, entityName
}
