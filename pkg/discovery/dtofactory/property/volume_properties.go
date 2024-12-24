package property

import (
	api "k8s.io/api/core/v1"

	"github.ibm.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	k8sVolumeName = "KubernetesVolumeName"
)

// Build entity properties for a volume. The name is the name of the volume shown inside Kubernetes cluster.
func BuildVolumeProperties(vol *api.PersistentVolume) *proto.EntityDTO_EntityProperty {
	propertyNamespace := k8sPropertyNamespace
	propertyName := k8sVolumeName
	propertyValue := vol.Name
	return &proto.EntityDTO_EntityProperty{
		Namespace: &propertyNamespace,
		Name:      &propertyName,
		Value:     &propertyValue,
	}
}
