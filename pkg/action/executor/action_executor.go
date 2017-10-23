package executor

import (
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type TurboActionExecutor interface {
	Execute(actionItem *proto.ActionItemDTO) error
}
