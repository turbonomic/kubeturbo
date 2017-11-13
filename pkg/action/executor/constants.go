package executor

import "time"

const (
	// Set the grace period to 0 for deleting the pod immediately.
	podDeletionGracePeriodDefault int64 = 0

	//TODO: set podDeletionGracePeriodMax > 0
	// currently, if grace > 0, there will be some retries and could cause timeout easily
	podDeletionGracePeriodMax int64 = 0

	DefaultNoneExistSchedulerName = "turbo-none-exist-scheduler"

	HigherK8sVersion = "1.6.0"

	defaultRetryLess = 3
	defaultRetryMore = 6

	defaultWaitLockTimeOut = time.Second * 300
	defaultWaitLockSleep   = time.Second * 10

	defaultPodCreateSleep            = time.Second * 30
	defaultUpdateSchedulerSleep      = time.Second * 20
	defaultCheckSchedulerSleep       = time.Second * 5
	defaultCheckUpdateReplicaSleep   = time.Second * 5
	defaultCheckUpdateReplicaTimeout = time.Second * 180
	defaultCheckUpdateReplicaRetry   = 36
	defaultUpdateReplicaSleep        = time.Second * 20
	defaultUpdateReplicaTimeout      = time.Second * 180
	defaultUpdateReplicaRetry        = 10
	defaultMoreGrace                 = time.Second * 20
)
