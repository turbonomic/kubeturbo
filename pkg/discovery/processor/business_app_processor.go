package processor

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	appv1beta1 "sigs.k8s.io/application/api/v1beta1"

	"github.com/golang/glog"
	"github.com/turbonomic/kubeturbo/pkg/cluster"
	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	util "github.com/turbonomic/kubeturbo/pkg/util"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
)

type BusinessAppProcessor struct {
	ClusterScraper cluster.ClusterScraperInterface
	KubeCluster    *repository.KubeCluster
}

func NewBusinessAppProcessor(clusterScraper cluster.ClusterScraperInterface,
	kubeCluster *repository.KubeCluster) *BusinessAppProcessor {
	return &BusinessAppProcessor{
		ClusterScraper: clusterScraper,
		KubeCluster:    kubeCluster,
	}
}

func (p *BusinessAppProcessor) ProcessBusinessApps() {
	resources := []schema.GroupVersionResource{
		{
			Group:    util.K8sApplicationGV.Group,
			Version:  util.K8sApplicationGV.Version,
			Resource: util.ApplicationResName,
		},
		{
			Group:    util.ArgoCDApplicationGV.Group,
			Version:  util.ArgoCDApplicationGV.Version,
			Resource: util.ApplicationResName,
		},
	}

	for _, res := range resources {
		appToComponentMap := p.ProcessAppsOfType(res)
		if p.KubeCluster.K8sAppToComponentMap == nil {
			p.KubeCluster.K8sAppToComponentMap = appToComponentMap
		} else {
			for key, val := range appToComponentMap {
				p.KubeCluster.K8sAppToComponentMap[key] = val
			}
		}
	}
	p.KubeCluster.ComponentToAppMap = inverseAppToComponentMap(p.KubeCluster.K8sAppToComponentMap)

}

func (p *BusinessAppProcessor) ProcessAppsOfType(res schema.GroupVersionResource) map[repository.K8sApp][]repository.K8sAppComponent {
	appToComponentMap := make(map[repository.K8sApp][]repository.K8sAppComponent)
	typedClusterScraper, isClusterScraper := p.ClusterScraper.(*cluster.ClusterScraper)
	if !isClusterScraper {
		// This is probably a test run
		return appToComponentMap
	}
	dynClient := typedClusterScraper.DynamicClient

	apps, err := dynClient.Resource(res).Namespace("").List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		glog.Warningf("Failed to list %v from %v/%v: %v", res.Resource, res.Group, res.Version, err)
		return appToComponentMap
	}

	for _, item := range apps.Items {
		var allEntities []repository.K8sAppComponent
		application := repository.K8sApp{
			Uid:       string(item.GetUID()),
			Namespace: item.GetNamespace(),
			Name:      item.GetName(),
		}

		switch res.Group {
		case util.K8sApplicationGV.Group:
			allEntities = p.getK8sAppEntities(item)
			application.Type = repository.AppTypeK8s
		case util.ArgoCDApplicationGV.Group:
			allEntities, err = p.getArgoCDAppEntities(item)
			if err != nil {
				glog.Warningf("Failed to get app entities for %v from %v/%v: %v", res.Resource, res.Group, res.Version, err)
				return appToComponentMap
			}
			application.Type = repository.AppTypeArgoCD
		}

		if len(allEntities) > 0 {
			appToComponentMap[application] = allEntities
			glog.V(4).Infof("Discovered %d entities for app:%s/%s, entities: %v", len(allEntities), item.GetNamespace(), item.GetName(), allEntities)
		} else {
			// nil indicates no entities discovered for this applicaiton
			appToComponentMap[application] = nil
		}
	}

	return appToComponentMap
}

func (p *BusinessAppProcessor) getK8sAppEntities(unstructuredApp unstructured.Unstructured) []repository.K8sAppComponent {
	app := appv1beta1.Application{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructuredApp.Object, &app); err != nil {
		glog.Warningf("Error converting unstructured app to typed app %v", err)
		return nil
	}

	selectors := app.Spec.Selector
	allEntities := []repository.K8sAppComponent{}
	for _, gk := range app.Spec.ComponentGroupKinds {
		entities, err := p.getEntitiesViaSelector(selectors, gk, app.Namespace)
		if err != nil {
			glog.Warningf("Error processing entities for application %s/%s, %v", app.Namespace, app.Name, err)
			continue
		}
		allEntities = append(allEntities, entities...)
	}

	return allEntities
}

func (p *BusinessAppProcessor) getArgoCDAppEntities(unstructuredApp unstructured.Unstructured) ([]repository.K8sAppComponent, error) {
	allEntities := []repository.K8sAppComponent{}
	resources, found, err := unstructured.NestedSlice(unstructuredApp.Object, "status", "resources")
	if err != nil || !found {
		return allEntities, fmt.Errorf("error retrieving resources from argocd app %s/%s: %v",
			unstructuredApp.GetNamespace(), unstructuredApp.GetName(), err)
	}

	for _, res := range resources {
		typedResource, ok := res.(map[string]interface{})
		if !ok {
			glog.Warningf("Decoded wrong resource detail from argocd app %s/%s: %T: %v",
				unstructuredApp.GetNamespace(), unstructuredApp.GetName(), res, res)
			continue
		}
		gk := metav1.GroupKind{
			Group: typedResource["group"].(string),
			Kind:  typedResource["kind"].(string),
		}
		entity, err := p.getEntity(gk, typedResource["name"].(string), typedResource["namespace"].(string))
		if err != nil {
			glog.Warningf("Error processing entities for argocd app %s/%s entity %v/%v, %v",
				unstructuredApp.GetNamespace(), unstructuredApp.GetName(), typedResource["group"], typedResource["group"], err)
			continue
		}
		allEntities = append(allEntities, *entity)
	}

	return allEntities, nil
}

func renderTypeInfo(gk metav1.GroupKind) (schema.GroupVersionResource, proto.EntityDTO_EntityType, error) {
	var res schema.GroupVersionResource
	var entityType proto.EntityDTO_EntityType
	switch gk.String() {
	// TODO: standardise this, or find a better way,
	// In case we continue to keep these values for gvr make them constants.
	case "StatefulSet.apps":
		res = schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "statefulsets"}
		entityType = proto.EntityDTO_WORKLOAD_CONTROLLER
	case "Deployment.apps":
		res = schema.GroupVersionResource{
			Group:    util.K8sAPIDeploymentGV.Group,
			Version:  util.K8sAPIDeploymentGV.Version,
			Resource: util.DeploymentResName}
		entityType = proto.EntityDTO_WORKLOAD_CONTROLLER
	case "ReplicaSet.apps":
		res = schema.GroupVersionResource{
			Group:    util.K8sAPIReplicasetGV.Group,
			Version:  util.K8sAPIReplicasetGV.Version,
			Resource: util.ReplicaSetResName}
		entityType = proto.EntityDTO_WORKLOAD_CONTROLLER
	case "DaemonSet.apps":
		res = schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "daemonsets"}
		entityType = proto.EntityDTO_WORKLOAD_CONTROLLER
	case "ReplicationController":
		res = schema.GroupVersionResource{
			Group:    util.K8sAPIReplicationControllerGV.Group,
			Version:  util.K8sAPIReplicationControllerGV.Version,
			Resource: util.ReplicationControllerResName}
		entityType = proto.EntityDTO_WORKLOAD_CONTROLLER
	case "Job.batch":
		res = schema.GroupVersionResource{
			Group:    "batch",
			Version:  "v1",
			Resource: "jobs"}
		entityType = proto.EntityDTO_WORKLOAD_CONTROLLER
	case "Service.v1":
		res = schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "services"}
		entityType = proto.EntityDTO_SERVICE
	case "Pod.v1":
		res = schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "pods"}
		entityType = proto.EntityDTO_CONTAINER_POD
	default:
		return res, entityType, fmt.Errorf("unsupport group kind type %s", gk.String())
	}

	return res, entityType, nil
}

func (p *BusinessAppProcessor) getEntitiesViaSelector(selector *metav1.LabelSelector, gk metav1.GroupKind, namespace string) ([]repository.K8sAppComponent, error) {
	res, entityType, err := renderTypeInfo(gk)
	if err != nil {
		return nil, err
	}
	dynClient := p.ClusterScraper.(*cluster.ClusterScraper).DynamicClient
	resourceList, err := dynClient.Resource(res).Namespace(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labels.Set(selector.MatchLabels).String()})
	if err != nil {
		return nil, err
	}

	entities := []repository.K8sAppComponent{}
	for _, r := range resourceList.Items {
		entity := repository.K8sAppComponent{
			EntityType: entityType,
			Uid:        string(r.GetUID()),
			Namespace:  r.GetNamespace(),
			Name:       r.GetName(),
		}

		entities = append(entities, entity)
	}

	return entities, nil
}

func (p *BusinessAppProcessor) getEntity(gk metav1.GroupKind, name, namespace string) (*repository.K8sAppComponent, error) {
	res, entityType, err := renderTypeInfo(gk)
	if err != nil {
		return nil, err
	}
	dynClient := p.ClusterScraper.(*cluster.ClusterScraper).DynamicClient
	object, err := dynClient.Resource(res).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return &repository.K8sAppComponent{
		EntityType: entityType,
		Uid:        string(object.GetUID()),
		Namespace:  object.GetNamespace(),
		Name:       object.GetName(),
	}, nil

}

func inverseAppToComponentMap(appToComponents map[repository.K8sApp][]repository.K8sAppComponent) map[repository.K8sAppComponent][]repository.K8sApp {
	entityToAppWithComponents := make(map[repository.K8sAppComponent][]repository.K8sApp)
	for app, components := range appToComponents {
		for _, component := range components {
			entityToAppWithComponents[component] = append(entityToAppWithComponents[component], app)
		}
	}
	return entityToAppWithComponents
}
