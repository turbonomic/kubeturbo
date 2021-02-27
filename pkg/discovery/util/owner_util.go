package util

import (
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Utilities to handle operations related to OwnerReference

type OwnerInfo struct {
	Kind string
	Name string
	Uid  string
}

// Get owner info by parsing the ownerReferences of an object
func GetOwnerInfo(owners []metav1.OwnerReference) OwnerInfo {
	for i := range owners {
		owner := &owners[i]
		if owner == nil || owner.Controller == nil {
			glog.Warningf("Nil OwnerReference")
			continue
		}
		if *(owner.Controller) && len(owner.Kind) > 0 && len(owner.Name) > 0 && len(owner.UID) > 0 {
			return OwnerInfo{owner.Kind, owner.Name, string(owner.UID)}
		}
	}
	return OwnerInfo{}
}

// Check if the input ownerInfo is empty
func IsOwnerInfoEmpty(ownerInfo OwnerInfo) bool {
	return ownerInfo.Kind == "" || ownerInfo.Name == "" || ownerInfo.Uid == ""
}
