package util

import (
	"strings"

	api "k8s.io/api/core/v1"

	"github.com/golang/glog"
)

// Find the appType (TODO the name is TBD) of the given pod.
// NOTE This function is highly depend on the name of different kinds of pod.
//
//	If a pod is created by a kubelet, then the name is like name-nodeName
//	If a pod is created by a replication controller, then the name is like name-random
//	if a pod is created by a deployment, then the name is like name-generated-random
func GetAppType(pod *api.Pod) string {
	if IsMirrorPod(pod) {
		nodeName := pod.Spec.NodeName
		na := strings.Split(pod.Name, nodeName)
		result := na[0]
		if len(result) > 1 {
			return result[:len(result)-1]
		}
		return result
	} else {
		ownerInfo, err := GetPodParentInfo(pod)
		if err != nil {
			glog.Errorf("fail to getAppType: %v", err.Error())
			return ""
		}
		if IsOwnerInfoEmpty(ownerInfo) {
			return pod.Name
		}

		//TODO: if parent.Kind==ReplicaSet:
		//       try to find the Deployment if it has.
		//      or extract the Deployment Name by string operations
		return ownerInfo.Name
	}
}
