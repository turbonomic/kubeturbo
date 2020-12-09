package processor

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	res := schema.GroupVersionResource{
		Group:    util.K8sApplicationGV.Group,
		Version:  util.K8sApplicationGV.Version,
		Resource: util.ApplicationResName}

	typedClusterScraper, isClusterScraper := p.ClusterScraper.(*cluster.ClusterScraper)
	if !isClusterScraper {
		// This is probably a test run
		return
	}
	dynClient := typedClusterScraper.DynamicClient

	apps, err := dynClient.Resource(res).Namespace("").List(metav1.ListOptions{})
	if err != nil {
		glog.Warningf("Error while processing application entities: %v", err)
		return
	}

	appToComponentMap := make(map[repository.K8sApp][]repository.K8sAppComponent)
	for _, item := range apps.Items {
		app := appv1beta1.Application{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &app); err != nil {
			glog.Warningf("Error converting unstructured app to typed app %v", err)
			continue
		}

		selectors := app.Spec.Selector
		allEntities := []repository.K8sAppComponent{}
		qualifiedAppName := fmt.Sprintf("%s/%s", app.Namespace, app.Name)
		for _, gk := range app.Spec.ComponentGroupKinds {
			entities, err := p.getEntities(selectors, gk, app.Namespace)
			if err != nil {
				glog.Warningf("Error processing entities for application %s, %v", qualifiedAppName, err)
				continue
			}
			allEntities = append(allEntities, entities...)
		}

		application := repository.K8sApp{
			Uid:       string(app.UID),
			Namespace: app.Namespace,
			Name:      app.Name,
		}
		if len(allEntities) > 0 {
			appToComponentMap[application] = allEntities
			glog.V(4).Infof("Discovered %d entities for app:%s, entities: %v", len(allEntities), qualifiedAppName, allEntities)
		} else {
			// nil indicates no entities discovered for this applicaiton
			appToComponentMap[application] = nil
		}
	}

	p.KubeCluster.K8sAppToComponentMap = appToComponentMap
	p.KubeCluster.ComponentToAppMap = inverseAppToComponentMap(appToComponentMap)
}

func (p *BusinessAppProcessor) getEntities(selector *metav1.LabelSelector, gk metav1.GroupKind, namespace string) ([]repository.K8sAppComponent, error) {
	res := schema.GroupVersionResource{}
	var err error
	var entityType proto.EntityDTO_EntityType
	switch gk.String() {
	// TODO: standardise this, find a better way,
	// for example move to using controller-runtime and the need for explicitly
	// mapping each type might not be there.
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
	// TODO: not sure why service gk returns "Service.v1"
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
		return nil, fmt.Errorf("unsupport group kind type %s", gk.String())
	}

	dynClient := p.ClusterScraper.(*cluster.ClusterScraper).DynamicClient
	resourceList, err := dynClient.Resource(res).Namespace(namespace).List(metav1.ListOptions{LabelSelector: labels.Set(selector.MatchLabels).String()})
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

func inverseAppToComponentMap(appToComponents map[repository.K8sApp][]repository.K8sAppComponent) map[repository.K8sAppComponent][]repository.K8sApp {
	entityToAppWithComponents := make(map[repository.K8sAppComponent][]repository.K8sApp)
	for app, components := range appToComponents {
		for _, component := range components {
			entityToAppWithComponents[component] = append(entityToAppWithComponents[component], app)
		}
	}
	return entityToAppWithComponents
}
