package executor

import (
	"github.com/turbonomic/kubeturbo/pkg/action/turboaction"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"time"
)

const (
	// Set the maximum timeout for pod deletion to 30 seconds.
	podDeletionTimeout time.Duration = time.Second * 30

	// This is the maximum timeout for action executor.
	secondPhaseTimeoutLimit time.Duration = time.Minute * 5
)

type TurboActionExecutor interface {
	Execute(actionItem *proto.ActionItemDTO) (*turboaction.TurboAction, error)
}
