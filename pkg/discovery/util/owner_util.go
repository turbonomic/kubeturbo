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

// GetOwnerInfo get owner info by parsing the ownerReference of an object
// A valid owner must be a managing controller, and have non-empty Kind, Name and Uid
// If there are multiple valid owners, pick the first one
func GetOwnerInfo(owners []metav1.OwnerReference) (OwnerInfo, bool) {
	var ownerInfo OwnerInfo
	var ownerSet bool
	for _, owner := range owners {
		if owner.Controller == nil || !(*owner.Controller) {
			glog.V(3).Infof("Owner %+v is not a managing controller.", owner)
			continue
		}
		if len(owner.Kind) > 0 && len(owner.Name) > 0 && len(owner.UID) > 0 {
			// Found a valid controller owner
			if ownerSet {
				glog.V(3).Infof("Multiple controller owners found: %+v", owner)
				continue
			}
			ownerInfo = OwnerInfo{
				Kind: owner.Kind,
				Name: owner.Name,
				Uid:  string(owner.UID),
			}
			ownerSet = true
		}
	}
	return ownerInfo, ownerSet
}

// IsOwnerInfoEmpty check if the input ownerInfo is empty
func IsOwnerInfoEmpty(ownerInfo OwnerInfo) bool {
	return ownerInfo.Kind == "" || ownerInfo.Name == "" || ownerInfo.Uid == ""
}
