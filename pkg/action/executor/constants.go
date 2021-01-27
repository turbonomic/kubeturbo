package executor

import (
	"time"
)

const (
	defaultRetryLess = 3
	defaultRetryMore = 55

	defaultRetrySleepInterval = time.Second * 3
	defaultRetryShortTimeout  = time.Second * 10

	defaultWaitLockTimeOut = time.Second * 300
	defaultWaitLockSleep   = time.Second * 10

	defaultPodCheckSleep      = time.Second * 10
	defaultPodCreateSleep     = time.Second * 11
	defaultUpdateReplicaSleep = time.Second * 20

	// this annotation is set for move/Resize actions;
	// which can be used for future garbage collection if action is interrupted
	TurboActionAnnotationKey   string = "kubeturbo.io/action"
	TurboMoveAnnotationValue   string = "move"
	TurboResizeAnnotationValue string = "resize"
	TurboGCLabelKey            string = "kubeturbo.io"
	TurboGCLabelVal            string = "gc"
)
