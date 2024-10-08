package probe

import (
	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

// Interface to perform execution of an action request for an entity in the TurboProbe.
// It receives a ActionExecutionDTO that contains the action request parameters. The target account values contain the
// information for connecting to the target environment to which the entity belongs. ActionProgressTracker will be used
// by the client to send periodic action progress updates to the server.
type TurboActionExecutorClient interface {
	ExecuteAction(actionExecutionDTO *proto.ActionExecutionDTO,
		accountValues []*proto.AccountValue,
		progressTracker ActionProgressTracker) (*proto.ActionResult, error)

	ExecuteActionList(actionExecutionDTOs []*proto.ActionExecutionDTO,
		accountValues []*proto.AccountValue,
		progressTracker ActionListProgressTracker) (*proto.ActionListResponse, error)
}

// Interface to send action progress to the server
type ActionProgressTracker interface {
	UpdateProgress(actionState proto.ActionResponseState, description string, progress int32)
}

// Interface to send action list progress to the server
type ActionListProgressTracker interface {
	UpdateProgress(actionOid int64, actionState proto.ActionResponseState, description string, progress int32)
}
