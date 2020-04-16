package task

import (
	api "k8s.io/api/core/v1"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/pborman/uuid"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
)

const (
	TaskSucceeded TaskResultState = "Succeeded"
	TaskFailed    TaskResultState = "Failed"
)

type Task struct {
	uid string

	nodeList []*api.Node
	podList  []*api.Pod
	cluster  *repository.ClusterSummary
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

// Assign cluster summary to the task.
func (t *Task) WithCluster(cluster *repository.ClusterSummary) *Task {
	t.cluster = cluster
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

func (t *Task) Cluster() *repository.ClusterSummary {
	return t.cluster
}

type TaskResultState string

// A TaskResult contains a state, indicate whether the task is finished successfully; a err if there is any; a list of
// EntityDTO.
type TaskResult struct {
	workerID         string
	state            TaskResultState
	err              error
	content          []*proto.EntityDTO
	namespaceMetrics []*repository.NamespaceMetrics
	entityGroups     []*repository.EntityGroup
	podEntities      []*repository.KubePod
	kubeControllers  []*repository.KubeController
}

func NewTaskResult(workerID string, state TaskResultState) *TaskResult {
	return &TaskResult{
		workerID: workerID,
		state:    state,
	}
}

func (r *TaskResult) WorkerId() string {
	return r.workerID
}

func (r *TaskResult) State() TaskResultState {
	return r.state
}

func (r *TaskResult) Content() []*proto.EntityDTO {
	return r.content
}

func (r *TaskResult) PodEntities() []*repository.KubePod {
	return r.podEntities
}

func (r *TaskResult) NamespaceMetrics() []*repository.NamespaceMetrics {
	return r.namespaceMetrics
}

func (r *TaskResult) EntityGroups() []*repository.EntityGroup {
	return r.entityGroups
}

func (r *TaskResult) KubeControllers() []*repository.KubeController {
	return r.kubeControllers
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

func (r *TaskResult) WithPodEntities(podEntities []*repository.KubePod) *TaskResult {
	r.podEntities = podEntities
	return r
}

func (r *TaskResult) WithNamespaceMetrics(namespaceMetrics []*repository.NamespaceMetrics) *TaskResult {
	r.namespaceMetrics = namespaceMetrics
	return r
}

func (r *TaskResult) WithEntityGroups(entityGroups []*repository.EntityGroup) *TaskResult {
	r.entityGroups = entityGroups
	return r
}

func (r *TaskResult) WithKubeControllers(kubeControllers []*repository.KubeController) *TaskResult {
	r.kubeControllers = kubeControllers
	return r
}
