package executor

import (
	"github.com/vmturbo/kubeturbo/pkg/action/turboaction"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

)

type TurboActionExecutor interface {
	Execute(actionItem *proto.ActionItemDTO) (*turboaction.TurboAction, error)
}
