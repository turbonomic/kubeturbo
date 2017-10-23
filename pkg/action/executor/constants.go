package executor

import "time"

const (
	// Set the grace period to 0 for deleting the pod immediately.
	podDeletionGracePeriodDefault int64 = 0

	//TODO: set podDeletionGracePeriodMax > 0
	// currently, if grace > 0, there will be some retries and could cause timeout easily
	podDeletionGracePeriodMax int64 = 0

	DefaultNoneExistSchedulerName = "turbo-none-exist-scheduler"
	kindReplicationController     = "ReplicationController"
	kindReplicaSet                = "ReplicaSet"
	kindDeployment                = "Deployment"

	HigherK8sVersion = "1.6.0"

	defaultRetryLess int = 3
	defaultRetryMore int = 6

	defaultWaitLockTimeOut = time.Second * 300
	defaultWaitLockSleep   = time.Second * 10

	defaultPodCreateSleep       = time.Second * 30
	defaultUpdateSchedulerSleep = time.Second * 20
	defaultCheckSchedulerSleep  = time.Second * 5
	defaultUpdateReplicaSleep   = time.Second * 20
	defaultMoreGrace            = time.Second * 20
)
