package util

import (
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/sets"
)

// Utilities to handle operations related to OwnerReference

type OwnerInfo struct {
	APIVersion string
	Kind       string
	Namespace  string
	Name       string
	Uid        string
	// Stores the name of containers in the parents pod template spec
	Containers sets.String
}

// GetOwnerInfo get owner info by parsing the ownerReference of an object
// A valid owner must be a managing controller, and have non-empty Kind, Name and Uid
// If there are multiple valid owners, pick the first one
func GetOwnerInfo(owners []metav1.OwnerReference) (OwnerInfo, bool) {
	var ownerInfo OwnerInfo
	var ownerSet bool
	for _, owner := range owners {
		if len(owner.Kind) > 0 && len(owner.Name) > 0 && len(owner.UID) > 0 {
			if IsController(owner) {
				glog.V(3).Infof("Found managing controller %+v.", owner)
				// This owner is also the controller, so we use this.
				return OwnerInfo{
					APIVersion: owner.APIVersion,
					Kind:       owner.Kind,
					Namespace:  ownerInfo.Namespace,
					Name:       owner.Name,
					Uid:        string(owner.UID),
				}, true
			}

			if ownerSet {
				continue
			}
			ownerSet = true
			ownerInfo = OwnerInfo{
				APIVersion: owner.APIVersion,
				Kind:       owner.Kind,
				Namespace:  ownerInfo.Namespace,
				Name:       owner.Name,
				Uid:        string(owner.UID),
			}
		}
	}

	glog.V(3).Infof("No managing controller was found, picked the first owner in list: %+v.", ownerInfo)
	return ownerInfo, ownerSet
}

func IsController(owner metav1.OwnerReference) bool {
	if owner.Controller != nil && (*owner.Controller) {
		return true
	}
	return false
}

// IsOwnerInfoEmpty check if the input ownerInfo is empty
func IsOwnerInfoEmpty(ownerInfo OwnerInfo) bool {
	return ownerInfo.Kind == "" || ownerInfo.Name == "" || ownerInfo.Uid == ""
}

func (ownerInfo *OwnerInfo) Unstructured() *unstructured.Unstructured {
	unstructuredOwnerInfo := &unstructured.Unstructured{}
	unstructuredOwnerInfo.SetAPIVersion(ownerInfo.APIVersion)
	unstructuredOwnerInfo.SetKind(ownerInfo.Kind)
	unstructuredOwnerInfo.SetNamespace(ownerInfo.Namespace)
	unstructuredOwnerInfo.SetName(ownerInfo.Name)
	return unstructuredOwnerInfo
}
