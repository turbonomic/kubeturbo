package turboaction

import (
	"fmt"
	"time"

	"k8s.io/kubernetes/pkg/api"
)

type VMTEventContentBuilder struct {
	actionType      string
	targetObject    *TargetObject
	parentObjectRef *ParentObjectRef
	actionSpec      ActionSpec
	messageID       int
}

func NewVMTEventContentBuilder(actionType string, targetObj *TargetObject, messageID int) *VMTEventContentBuilder {
	return &VMTEventContentBuilder{
		actionType:   actionType,
		targetObject: targetObj,
		messageID:    messageID,
	}
}

func (this *VMTEventContentBuilder) Build() TurboActionContent {
	content := TurboActionContent{
		ActionType:   this.actionType,
		TargetObject: *(this.targetObject),
		ActionSpec:   this.actionSpec,
		ActionMessageID: this.messageID,
	}
	if this.parentObjectRef != nil {
		content.ParentObjectRef = *(this.parentObjectRef)
	}
	return content
}

func (this *VMTEventContentBuilder) ParentObjectRef(parentObjRef *ParentObjectRef) *VMTEventContentBuilder {
	this.parentObjectRef = parentObjRef
	return this
}

func (this *VMTEventContentBuilder) ActionSpec(actionSpec ActionSpec) *VMTEventContentBuilder {
	this.actionSpec = actionSpec
	return this
}

type VMTEventBuilder struct {
	namespace string
	status    TurboActionStatus
	content   TurboActionContent
}

func NewVMTEventBuilder(namespace string) *VMTEventBuilder {
	ns := namespace
	if ns == "" {
		ns = api.NamespaceDefault
	}
	return &VMTEventBuilder{
		namespace: ns,
		status:    Pending,
	}
}

func (this *VMTEventBuilder) Content(content TurboActionContent) *VMTEventBuilder {
	this.content = content
	return this
}

func (this *VMTEventBuilder) Status(status TurboActionStatus) *VMTEventBuilder {
	this.status = status
	return this
}

func (this *VMTEventBuilder) Create() TurboAction {
	t := time.Now()

	return TurboAction{
		TypeMeta: TypeMeta{
			Kind: "VMTEvent",
		},
		ObjectMeta: ObjectMeta{
			Name:      fmt.Sprintf("%v.%x", this.content.TargetObject.TargetObjectName, t.UnixNano()),
			Namespace: this.namespace,
		},
		Status:  this.status,
		Content: this.content,

		FirstTimestamp: t,
		LastTimestamp:  t,
	}
}
