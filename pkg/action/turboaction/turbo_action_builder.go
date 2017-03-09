package turboaction

import (
	"fmt"
	"time"

	"k8s.io/kubernetes/pkg/api"
)

type TurboActionContentBuilder struct {
	actionType      TurboActionType
	targetObject    *TargetObject
	parentObjectRef *ParentObjectRef
	actionSpec      ActionSpec
}

func NewTurboActionContentBuilder(actionType TurboActionType, targetObj *TargetObject) *TurboActionContentBuilder {
	return &TurboActionContentBuilder{
		actionType:   actionType,
		targetObject: targetObj,
	}
}

func (b *TurboActionContentBuilder) Build() TurboActionContent {
	content := TurboActionContent{
		ActionType:   b.actionType,
		TargetObject: *(b.targetObject),
		ActionSpec:   b.actionSpec,
	}
	if b.parentObjectRef != nil {
		content.ParentObjectRef = *(b.parentObjectRef)
	}
	return content
}

func (b *TurboActionContentBuilder) ParentObjectRef(parentObjRef *ParentObjectRef) *TurboActionContentBuilder {
	b.parentObjectRef = parentObjRef
	return b
}

func (b *TurboActionContentBuilder) ActionSpec(actionSpec ActionSpec) *TurboActionContentBuilder {
	b.actionSpec = actionSpec
	return b
}

type TurboActionBuilder struct {
	namespace string
	uid       UID
	status    TurboActionStatus
	content   TurboActionContent
}

func NewTurboActionBuilder(namespace string, uid string) *TurboActionBuilder {
	ns := namespace
	if ns == "" {
		ns = api.NamespaceDefault
	}
	return &TurboActionBuilder{
		namespace: ns,
		uid:       UID(uid),
		status:    Pending,
	}
}

func (tab *TurboActionBuilder) Content(content TurboActionContent) *TurboActionBuilder {
	tab.content = content
	return tab
}

func (tab *TurboActionBuilder) Status(status TurboActionStatus) *TurboActionBuilder {
	tab.status = status
	return tab
}

func (tab *TurboActionBuilder) Create() TurboAction {
	t := time.Now()

	return TurboAction{
		TypeMeta: TypeMeta{
			Kind: "TurboAction",
		},
		ObjectMeta: ObjectMeta{
			Name:      fmt.Sprintf("%v.%x", tab.content.TargetObject.TargetObjectName, t.UnixNano()),
			Namespace: tab.namespace,
			UID:       tab.uid,
		},
		Status:  tab.status,
		Content: tab.content,

		FirstTimestamp: t,
		LastTimestamp:  t,
	}
}
