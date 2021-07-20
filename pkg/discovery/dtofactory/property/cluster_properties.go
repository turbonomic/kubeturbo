package property

import (
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	vcpuUnit          = "VCPUUnit"
	unitTypeMillicore = "Millicore"
)

// Build the cluster property to depict this cluster now uses millicores as units for vcpu & related commodities.
func BuildClusterProperty() []*proto.EntityDTO_EntityProperty {
	var properties []*proto.EntityDTO_EntityProperty
	propertyNamespace := k8sPropertyNamespace
	propertyName := vcpuUnit
	propertyValue := unitTypeMillicore
	return append(properties, &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &propertyName,
		Value:     &propertyValue,
	})
}
