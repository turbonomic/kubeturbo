package util

import "k8s.io/apimachinery/pkg/runtime/schema"

const (
	KindReplicationController = "ReplicationController"
	KindReplicaSet            = "ReplicaSet"
	KindDeployment            = "Deployment"

	K8sExtensionsGroupName = "extensions"
	K8sAppsGroupName       = "apps"

	ReplicationControllerResName = "replicationcontrollers"
	ReplicaSetResName            = "replicasets"
	DeploymentResName            = "deployments"
)

// The API group version under which deployments and replicasets are exposed by the k8s cluster
var K8sAPIDeploymentReplicasetGV = schema.GroupVersion{Group: K8sAppsGroupName, Version: "v1"}

// The API group under which replicationcontrollers are exposed by the k8s server
var K8sAPIReplicationControllerGV = schema.GroupVersion{Group: "", Version: "v1"}
