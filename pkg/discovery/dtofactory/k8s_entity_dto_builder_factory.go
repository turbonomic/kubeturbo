package dtofactory

import (
	"fmt"
	"k8s.io/kubernetes/pkg/runtime"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/turbonomic/kubeturbo/pkg/discovery/metrics"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
)

type k8sEntityDTOBuilder interface {
	BuildEntityDTOs(objects []runtime.Object) ([]*proto.EntityDTO, error)
}

func GetK8sEntityDTOBuilder(eType task.DiscoveredEntityType, sink *metrics.EntityMetricSink) (k8sEntityDTOBuilder,
	error) {
	switch eType {
	case task.NodeType:
		return newNodeEntityDTOBuilder(sink), nil
	}
	return nil, fmt.Errorf("Cannot find entity dto builder for %s", eType)
}
