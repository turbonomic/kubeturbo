package dtofactory

import (
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

)

type k8sEntityDTOBuilder interface {
	BuildEntityDTOs(objects []runtime.Object) ([]*proto.EntityDTO, error)
}
