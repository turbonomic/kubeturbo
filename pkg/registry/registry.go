package registry

import (
	"github.com/vmturbo/kubeturbo/pkg/storage/vmtruntime"
)

// GetList returns an empty list of certain kind.
func GetList(resource string) vmtruntime.VMTObject {
	if resource == "vmtevents" {
		return &VMTEventList{}
	}

	return &FakeVMTObject{}
}

type FakeVMTObject struct{}

func (*FakeVMTObject) IsVMTObject() {}
