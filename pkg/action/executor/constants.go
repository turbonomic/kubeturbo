package executor

import (
	"time"
)

const (
	// Default Number of Retries for making changes during action execution
	DefaultExecutionRetry      = 3
	// Default number of Retries for waiting for Pod to be ready during action execution
	DefaultWaitForPodThreshold = 55

	DefaultRetrySleepInterval = time.Second * 3
	DefaultRetryShortTimeout  = time.Second * 20
	DefaultRetryTimeout       = time.Second * 120

	defaultWaitLockTimeOut = time.Second * 300
	defaultWaitLockSleep   = time.Second * 10

	defaultPodCreateSleep     = time.Second * 10
	defaultUpdateReplicaSleep = time.Second * 20

	// this annotation is set for move/Resize actions;
	// which can be used for future garbage collection if action is interrupted
	TurboActionAnnotationKey   string = "kubeturbo.io/action"
	TurboMoveAnnotationValue   string = "move"
	TurboResizeAnnotationValue string = "resize"
	TurboGCLabelKey            string = "kubeturbo.io"
	TurboGCLabelVal            string = "gc"

	DummyScheduler   string = "turbo-scheduler"
	DefaultScheduler string = "default-scheduler"
)
