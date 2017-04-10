package task

import (
	"k8s.io/kubernetes/pkg/api"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/pborman/uuid"
)

const (
	ClusterType         DiscoveredEntityType = "Cluster"
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

	nodeList []api.Node
	podList  []api.Pod
}

// Worker task is consisted of a list of nodes the worker must discover.
func NewTask() *Task {
	return &Task{
		uid: uuid.NewUUID().String(),
	}
}

// Assign nodes to the task.
func (t *Task) WithNodes(nodeList []api.Node) *Task {
	return &Task{
		nodeList: nodeList,
	}
}

// Assign pods to the task.
func (t *Task) WithPods(podList []api.Pod) *Task {
	return &Task{
		podList: podList,
	}
}

// Get node list from the task.
func (t *Task) NodeList() []api.Node {
	return t.nodeList
}

// Get pod list from the task.
func (t *Task) PodList() []api.Pod {
	return t.podList
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
