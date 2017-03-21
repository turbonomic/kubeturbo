package executor

import (
	"github.com/vmturbo/kubeturbo/pkg/action/turboaction"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"time"
)

const (
	// This is the maximum time for action executor
	secondPhaseTimeoutLimit time.Duration = time.Minute * 5
)

type TurboActionExecutor interface {
	Execute(actionItem *proto.ActionItemDTO) (*turboaction.TurboAction, error)
}
