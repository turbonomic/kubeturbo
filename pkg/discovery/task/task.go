package task

import (
	"strings"

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
	uid         string
	name        string
	node        *api.Node
	pendingPods []*api.Pod
	runningPods []*api.Pod
	pods        []*api.Pod
	pvs         []*api.PersistentVolume
	pvcs        []*api.PersistentVolumeClaim
	cluster     *repository.ClusterSummary
}

// Worker task is consisted of a list of nodes the worker must discover.
func NewTask() *Task {
	uid := uuid.NewUUID().String()
	name := strings.Split(uid, "-")[0]
	return &Task{
		uid:  uid,
		name: name,
	}
}

func (t *Task) WithNode(node *api.Node) *Task {
	t.node = node
	return t
}

// Assign pods to the task.
func (t *Task) WithPods(pods []*api.Pod) *Task {
	t.pods = pods
	return t
}

func (t *Task) WithRunningPods(runningPods []*api.Pod) *Task {
	t.runningPods = runningPods
	t.pods = append(t.pods, runningPods...)
	return t
}

func (t *Task) WithPendingPods(pendingPods []*api.Pod) *Task {
	t.pendingPods = pendingPods
	t.pods = append(t.pods, pendingPods...)
	return t
}

// Assign pvs to the task.
func (t *Task) WithPVs(pvs []*api.PersistentVolume) *Task {
	t.pvs = pvs
	return t
}

// Assign pvcs to the task.
func (t *Task) WithPVCs(pvcs []*api.PersistentVolumeClaim) *Task {
	t.pvcs = pvcs
	return t
}

// Assign cluster summary to the task.
func (t *Task) WithCluster(cluster *repository.ClusterSummary) *Task {
	t.cluster = cluster
	return t
}

// Get node from the task.
func (t *Task) Node() *api.Node {
	return t.node
}

// Get pod list from the task.
func (t *Task) PodList() []*api.Pod {
	return t.pods
}

func (t *Task) RunningPodList() []*api.Pod {
	return t.runningPods
}

func (t *Task) PendingPodList() []*api.Pod {
	return t.pendingPods
}

// Get PV list from the task.
func (t *Task) PVList() []*api.PersistentVolume {
	return t.pvs
}

// Get PVC list from the task.
func (t *Task) PVCList() []*api.PersistentVolumeClaim {
	return t.pvcs
}

func (t *Task) Cluster() *repository.ClusterSummary {
	return t.cluster
}

func (t *Task) String() string {
	return "[id: " + t.name + ", node: " + t.node.GetName() + "]"
}

type TaskResultState string

// A TaskResult contains a state, indicate whether the task is finished successfully; a err if there is any; a list of
// EntityDTO.
type TaskResult struct {
	workerID              string
	state                 TaskResultState
	err                   error
	content               []*proto.EntityDTO
	namespaceMetrics      []*repository.NamespaceMetrics
	entityGroups          []*repository.EntityGroup
	podEntities           []*repository.KubePod
	kubeControllers       []*repository.KubeController
	containerSpecMetrics  []*repository.ContainerSpecMetrics
	podVolumeMetrics      []*repository.PodVolumeMetrics
	sidecarContainerSpecs []string
	podsWithVolumes       []string
	notReadyNodes         []string
	mirrorPodUids         []string
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

func (r *TaskResult) ContainerSpecMetrics() []*repository.ContainerSpecMetrics {
	return r.containerSpecMetrics
}

func (r *TaskResult) PodVolumeMetrics() []*repository.PodVolumeMetrics {
	return r.podVolumeMetrics
}

func (r *TaskResult) SidecarContainerSpecs() []string {
	return r.sidecarContainerSpecs
}

func (r *TaskResult) PodWithVolumes() []string {
	return r.podsWithVolumes
}

func (r *TaskResult) NotReadyNodes() []string {
	return r.notReadyNodes
}

func (r *TaskResult) MirrorPodUids() []string {
	return r.mirrorPodUids
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

func (r *TaskResult) WithContainerSpecMetrics(containerSpecMetrics []*repository.ContainerSpecMetrics) *TaskResult {
	r.containerSpecMetrics = containerSpecMetrics
	return r
}

func (r *TaskResult) WithPodVolumeMetrics(podVolumeMetrics []*repository.PodVolumeMetrics) *TaskResult {
	r.podVolumeMetrics = podVolumeMetrics
	return r
}

func (r *TaskResult) WithSidecarContainerSpecs(sidecarContainerSpecs []string) *TaskResult {
	r.sidecarContainerSpecs = sidecarContainerSpecs
	return r
}

func (r *TaskResult) WithPodsWithVolumes(podsWithVolumes []string) *TaskResult {
	r.podsWithVolumes = podsWithVolumes
	return r
}

func (r *TaskResult) WithNotReadyNodes(notReadyNodes []string) *TaskResult {
	r.notReadyNodes = notReadyNodes
	return r
}

func (r *TaskResult) WithMirrorPodUids(mirrorPodUids []string) *TaskResult {
	r.mirrorPodUids = mirrorPodUids
	return r
}
