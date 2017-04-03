package task

import (
	"k8s.io/kubernetes/pkg/api"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	NodeType        DiscoveredEntityType = "Node"
	PodType         DiscoveredEntityType = "Pod"
	ApplicationType DiscoveredEntityType = "Application"
	ServiceType     DiscoveredEntityType = "Service"

	TaskSucceeded TaskResultState = "Succeeded"
	TaskFailed    TaskResultState = "Failed"
)

type DiscoveredEntityType string

type WorkerTask struct {
	nodeList []api.Node
}

// Worker task is consisted of a list of nodes the worker must discover.
func NewWorkerTask(nodelist []api.Node) *WorkerTask {
	return &WorkerTask{
		nodeList: nodelist,
	}
}

func (t *WorkerTask) NodeList() []api.Node{
	return t.nodeList
}

type TaskResultState string

// A TaskResult contains a state, indicate whether the task is finished successfully; a err if there is any; a list of
// EntityDTO.
type TaskResult struct {
	State TaskResultState
	Err   error

	Content []*proto.EntityDTO
}

func NewTaskResult(state TaskResultState) *TaskResult {
	return &TaskResult{
		State: state,
	}
}

func (r *TaskResult) SetErr(err error) *TaskResult {
	r.Err = err
	return r
}

func (r *TaskResult) SetContent(entityDTOs []*proto.EntityDTO) *TaskResult {
	r.Content = entityDTOs
	return r
}
