package k8sappcomponents

import (
	"fmt"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	sdkbuilder "github.com/turbonomic/turbo-go-sdk/pkg/builder"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

const (
	applicationCommodityDefaultCapacity = 1e10
)

// Affinity processor parses each affinity rule defined in pod and creates commodityDTOs for nodes and pods.
type K8sAppComponentsProcessor struct {
	appComponents map[repository.K8sAppComponent][]repository.K8sApp
}

func NewK8sAppComponentsProcessor(appComponents map[repository.K8sAppComponent][]repository.K8sApp) *K8sAppComponentsProcessor {
	return &K8sAppComponentsProcessor{
		appComponents: appComponents,
	}
}

func (k *K8sAppComponentsProcessor) ProcessAppComponentDTOs(entityDTOs []*proto.EntityDTO) {
	for component, apps := range k.appComponents {
		for _, entityDTO := range entityDTOs {
			// This assumes that the UID of the k8s resource doesn't change within the same discovery cycle.
			if component.Uid == entityDTO.GetId() {
				// This does an in place update of the entityDTO
				k.sellCommodities(entityDTO, component, apps)
			}
		}
	}
}

func (k *K8sAppComponentsProcessor) sellCommodities(entityDTO *proto.EntityDTO, component repository.K8sAppComponent,
	apps []repository.K8sApp) {
	for _, app := range apps {
		key := fmt.Sprintf("%s-%s/%s-%s/%s", "App", app.Namespace, app.Name,
			component.EntityType.String(), component.Name)

		commoditySold, err := sdkbuilder.NewCommodityDTOBuilder(proto.CommodityDTO_APPLICATION).
			Key(key).
			Capacity(applicationCommodityDefaultCapacity).
			Create()
		if err != nil {
			glog.Errorf("Error creating sold commodity for App Component %s/%s for app %s: %v",
				component.Namespace, component.Name, app.Name, err)
			continue
		}

		commoditiesSold := entityDTO.GetCommoditiesSold()
		commoditiesSold = append(commoditiesSold, commoditySold)
		entityDTO.CommoditiesSold = commoditiesSold
	}
}
