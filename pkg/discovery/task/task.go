package task

import (
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/pborman/uuid"
)

const (
	ClusterType     DiscoveredEntityType = "Cluster"
	NodeType        DiscoveredEntityType = "Node"
	PodType         DiscoveredEntityType = "Pod"
	ApplicationType DiscoveredEntityType = "Application"
	ServiceType     DiscoveredEntityType = "Service"

	TaskSucceeded TaskResultState = "Succeeded"
	TaskFailed    TaskResultState = "Failed"
)

type DiscoveredEntityType string

type Task struct {
	uid string

	nodeList []*api.Node
	podList  []*api.Pod
}

// Worker task is consisted of a list of nodes the worker must discover.
func NewTask() *Task {
	return &Task{
		uid: uuid.NewUUID().String(),
	}
}

// Assign nodes to the task.
func (t *Task) WithNodes(nodeList []*api.Node) *Task {
	t.nodeList = nodeList
	return t
}

// Assign pods to the task.
func (t *Task) WithPods(podList []*api.Pod) *Task {
	t.podList = podList
	return t
}

// Get node list from the task.
func (t *Task) NodeList() []*api.Node {
	return t.nodeList
}

// Get pod list from the task.
func (t *Task) PodList() []*api.Pod {
	return t.podList
}

type TaskResultState string

// A TaskResult contains a state, indicate whether the task is finished successfully; a err if there is any; a list of
// EntityDTO.
type TaskResult struct {
	workerID string
	state    TaskResultState
	err      error

	content []*proto.EntityDTO
}

func NewTaskResult(workerID string, state TaskResultState) *TaskResult {
	return &TaskResult{
		workerID: workerID,
		state:    state,
	}
}

func (r *TaskResult) State() TaskResultState {
	return r.state
}

func (r *TaskResult) Content() []*proto.EntityDTO {
	return r.content
}

func (r *TaskResult) Err() error {
	return r.err
}

func (r *TaskResult) WithErr(err error) *TaskResult {
	r.err = err
	return r
}

func (r *TaskResult) WithContent(entityDTOs []*proto.EntityDTO) *TaskResult {
	r.content = entityDTOs
	return r
}
