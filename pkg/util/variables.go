package util

import "k8s.io/apimachinery/pkg/runtime/schema"

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

	K8sExtensionsGroupName     = "extensions"
	K8sAppsGroupName           = "apps"
	K8sApplicationGroupName    = "app.k8s.io"
	ArgoCDApplicationGroupName = "argoproj.io"
	OpenShiftAppsGroupName     = "apps.openshift.io"
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
	K8sAPICronJobGV = schema.GroupVersion{Group: K8sBatchGroupName, Version: "v1beta1"}

	// Number of items that should be requested in each workload controller
	// list API to ensure no OOMs occur.
	// This value is calculated from cgroup MEMLIMIT using the below expression:
	// (mem_limit_gb * 0.9 - 0.5) * 5K; where m is memlimit in GB
	//
	// In the absence of any MEMLIMIT set, we limit the number of resources requested in
	// each call to fill up max of 8GB =(mem_limit_gb * 0.9 - 0.5) * 5000 = 33500
	// This default is quite unlikely to be used as Cgroup limit will always be available
	// atleast on linux based systems. If the container limit is not set, cgroup limit will
	// be available as nodes limit.
	// This will be used for non linux systems, eg kubeturbo local run on mac.
	ItemsPerListQuery = 33500
)
