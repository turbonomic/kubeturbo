package registry

import (
	"fmt"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/unversioned"
)

type VMTEventContentBuilder struct {
	actionType string
	targetSE   string
	moveSpec   MoveEventSpec
	scaleSpec  ScaleEventSpec
	messageID  int
}

func NewVMTEventContentBuilder(actionType string, targetSE string, messageID int) *VMTEventContentBuilder {
	return &VMTEventContentBuilder{
		actionType: actionType,
		targetSE:   targetSE,
		messageID:  messageID,
	}
}

func (this *VMTEventContentBuilder) Build() VMTEventContent {
	return VMTEventContent{
		ActionType:   this.actionType,
		TargetSE:     this.targetSE,
		VMTMessageID: this.messageID,
		MoveSpec:     this.moveSpec,
		ScaleSpec:    this.scaleSpec,
	}
}

func (this *VMTEventContentBuilder) MoveSpec(source, destination string) *VMTEventContentBuilder {
	moveSpec := MoveEventSpec{
		Source:      source,
		Destination: destination,
	}
	this.moveSpec = moveSpec
	return this
}

func (this *VMTEventContentBuilder) ScaleSpec(oldReplicas, newReplicas int) *VMTEventContentBuilder {
	scaleSpec := ScaleEventSpec{
		OriginalReplicas: oldReplicas,
		NewReplicas:      newReplicas,
	}
	this.scaleSpec = scaleSpec
	return this
}

type VMTEventBuilder struct {
	namespace string
	content   VMTEventContent
}

func NewVMTEventBuilder(namespace string) *VMTEventBuilder {
	ns := namespace
	if ns == "" {
		ns = api.NamespaceDefault
	}
	return &VMTEventBuilder{
		namespace: ns,
	}
}

func (this *VMTEventBuilder) Content(content VMTEventContent) *VMTEventBuilder {
	this.content = content
	return this
}

func (this *VMTEventBuilder) Create() VMTEvent {
	t := unversioned.Now()

	return VMTEvent{
		TypeMeta: TypeMeta{
			Kind: "VMTEvent",
		},
		ObjectMeta: ObjectMeta{
			Name:      fmt.Sprintf("%v.%x", this.content.TargetSE, t.UnixNano()),
			Namespace: this.namespace,
		},
		Content: this.content,

		FirstTimestamp: t,
		LastTimestamp:  t,
	}
}
