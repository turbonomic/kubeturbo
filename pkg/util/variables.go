package util

import (
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// Kubernetes workload controller types
	KindCronJob               = "CronJob"
	KindDaemonSet             = "DaemonSet"
	KindDeployment            = "Deployment"
	KindJob                   = "Job"
	KindReplicaSet            = "ReplicaSet"
	KindDeploymentConfig      = "DeploymentConfig"
	KindReplicationController = "ReplicationController"
	KindStatefulSet           = "StatefulSet"
	KindClusterRole           = "ClusterRole"
	KindRole                  = "Role"

	K8sExtensionsGroupName     = "extensions"
	K8sAppsGroupName           = "apps"
	K8sApplicationGroupName    = "app.k8s.io"
	ArgoCDApplicationGroupName = "argoproj.io"
	K8sBatchGroupName          = "batch"

	ReplicationControllerResName = "replicationcontrollers"
	ReplicaSetResName            = "replicasets"
	DeploymentResName            = "deployments"
	DeploymentConfigResName      = "deploymentconfigs"
	CronJobResName               = "cronjobs"
	JobResName                   = "jobs"
	StatefulSetResName           = "statefulsets"
	DaemonSetResName             = "daemonsets"
	ApplicationResName           = "applications"
	PodResName                   = "pods"

	OpenShiftAppsGroupName     = "apps.openshift.io"
	OpenShiftSecurityGroupName = "security.openshift.io"
	AuthorizationGroupName     = "rbac.authorization.k8s.io"
	OpenShiftSCCResName        = "securitycontextconstraints"

	ClusterRoleResName        = "clusterroles"
	ClusterRoleBindingResName = "clusterrolebindings"
	ServiceAccountResName     = "serviceaccounts"

	VerbCreate      = "create"
	VerbUpdate      = "update"
	VerbImpersonate = "impersonate"
	VerbDelete      = "delete"
	VerbUse         = "use"

	TransientRetryTimes = 5
	QuickRetryInterval  = time.Second * 2
)

var (
	// The API group version under which deployments and replicasets are exposed by the k8s cluster as of today
	K8sAPIDeploymentReplicasetDefaultGV = schema.GroupVersion{Group: K8sAppsGroupName, Version: "v1"}
	// The API group version under which deployments are exposed by the k8s cluster
	K8sAPIDeploymentGV = schema.GroupVersion{Group: K8sAppsGroupName, Version: "v1"}
	// The API group version under which replicasets are exposed by the k8s cluster
	K8sAPIReplicasetGV = schema.GroupVersion{Group: K8sAppsGroupName, Version: "v1"}
	// The API group under which replicationcontrollers are exposed by the k8s server
	// We do not discover the latest GV for this as we know that it has matured under core/v1
	K8sAPIReplicationControllerGV = schema.GroupVersion{Group: "", Version: "v1"}
	// The API group under which openshifts deploymentconfig resource is exposed by the server
	OpenShiftAPIDeploymentConfigGV = schema.GroupVersion{Group: OpenShiftAppsGroupName, Version: "v1"}
	// The API group under which application crd resource is installed on the server
	K8sApplicationGV = schema.GroupVersion{Group: K8sApplicationGroupName, Version: "v1beta1"}
	// The API group under which ArgoCD application crd resource is installed on the server
	ArgoCDApplicationGV = schema.GroupVersion{Group: ArgoCDApplicationGroupName, Version: "v1alpha1"}
	// The API group under which statefulsets are exposed by the k8s cluster
	K8sAPIStatefulsetGV = schema.GroupVersion{Group: K8sAppsGroupName, Version: "v1"}
	// The API group under which daemonsets are exposed by the k8s cluster
	K8sAPIDaemonsetGV = schema.GroupVersion{Group: K8sAppsGroupName, Version: "v1"}
	// The API group under which Job are exposed by the k8s cluster
	K8sAPIJobGV = schema.GroupVersion{Group: K8sBatchGroupName, Version: "v1"}
	// The API group under which CronJob are exposed by the k8s cluster
	K8sAPICronJobGV = schema.GroupVersion{Group: K8sBatchGroupName, Version: "v1"}
	// The API group under which openshifts scc resource is exposed by the server
	OpenShiftAPISCCGV = schema.GroupVersion{Group: OpenShiftSecurityGroupName, Version: "v1"}

	// This map stores the kubeturbo created service account name for each given scc
	// The service account name will be in the format "system:serviceaccount:<ns>:<name>"
	SCCMapping map[string]string = make(map[string]string)
)
