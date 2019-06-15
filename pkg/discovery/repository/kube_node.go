package repository

import (
	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	"k8s.io/api/core/v1"
)

// The pod in the cluster
type KubePod struct {
	*KubeEntity
	*v1.Pod

	// CPU frequency of the node hosting the pod, used to compute the pod cpu metrics
	NodeCpuFrequency float64
	ServiceId        string
	PodClusterId     string

	// container container Id
	ContainerApps map[string]*proto.EntityDTO
}

// The pod in the cluster
type KubeContainer struct {
	*KubeEntity
	*v1.Container
}

func NewKubePod(apiPod *v1.Pod, node *KubeNode, clusterName string) *KubePod {
	entity := NewKubeEntity(metrics.PodType, clusterName,
		apiPod.ObjectMeta.Namespace, apiPod.ObjectMeta.Name,
		string(apiPod.ObjectMeta.UID))

	podEntity := &KubePod{
		KubeEntity:    entity,
		Pod:           apiPod,
		PodClusterId:  util.BuildK8sEntityClusterID(apiPod.Namespace, apiPod.Name),
		ContainerApps: make(map[string]*proto.EntityDTO),
	}

	podEntity.NodeCpuFrequency = node.NodeCpuFrequency

	return podEntity
}
