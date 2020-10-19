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
)

type ApplicationProcessor struct {
	ClusterScraper cluster.ClusterScraperInterface
	KubeCluster    *repository.KubeCluster
}

func NewApplicationProcessor(clusterScraper cluster.ClusterScraperInterface,
	kubeCluster *repository.KubeCluster) *ApplicationProcessor {
	return &ApplicationProcessor{
		ClusterScraper: clusterScraper,
		KubeCluster:    kubeCluster,
	}
}

func (p *ApplicationProcessor) ProcessApplications() {
	res := schema.GroupVersionResource{
		Group:    util.K8sApplicationGV.Group,
		Version:  util.K8sApplicationGV.Version,
		Resource: util.ApplicationResName}

	dynClient := p.ClusterScraper.(*cluster.ClusterScraper).DynamicClient

	apps, err := dynClient.Resource(res).Namespace("").List(metav1.ListOptions{})
	if err != nil {
		glog.Warningf("Error while processing application entities: %v", err)
	}

	appToEntityMap := make(map[string][]repository.AppEntity)
	for _, item := range apps.Items {
		app := appv1beta1.Application{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &app); err != nil {
			glog.Warningf("Error converting unstructured app to typed app %v", err)
			continue
		}

		selectors := app.Spec.Selector
		allEntities := []repository.AppEntity{}
		qualifiedAppName := fmt.Sprintf("%s/%s", app.Namespace, app.Name)
		for _, gk := range app.Spec.ComponentGroupKinds {
			entities, err := p.getEntities(selectors, gk, app.Namespace)
			if err != nil {
				glog.Warningf("Error processing entities for application %s, %v", qualifiedAppName, app.Name, err)
				continue
			}
			allEntities = append(allEntities, entities...)
		}

		if len(allEntities) > 0 {
			appToEntityMap[qualifiedAppName] = allEntities
			glog.V(4).Infof("Discovered %d entities for app:%s, entities: %v", len(allEntities), qualifiedAppName, allEntities)
		} else {
			// nil indicates no entities discovered for this applicaiton
			appToEntityMap[qualifiedAppName] = nil
		}
	}

	p.KubeCluster.AppToEntityMap = appToEntityMap
}

func (p *ApplicationProcessor) getEntities(selector *metav1.LabelSelector, gk metav1.GroupKind, namespace string) ([]repository.AppEntity, error) {
	res := schema.GroupVersionResource{}
	var err error
	turboType := ""
	switch gk.String() {
	// TODO: standardise this, find a better way
	// Move to using controller-runtime and the need for explicitly
	// mapping each type might not be there.
	case "StatefulSet.apps":
		res = schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "statefulsets"}
		turboType = "workloadcontroller"
	case "Deployment.apps":
		res = schema.GroupVersionResource{
			Group:    util.K8sAPIDeploymentGV.Group,
			Version:  util.K8sAPIDeploymentGV.Version,
			Resource: util.DeploymentResName}
		turboType = "workloadcontroller"
	case "ReplicaSet.apps":
		res = schema.GroupVersionResource{
			Group:    util.K8sAPIReplicasetGV.Group,
			Version:  util.K8sAPIReplicasetGV.Version,
			Resource: util.ReplicaSetResName}
		turboType = "workloadcontroller"
	case "DaemonSet.apps":
		res = schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "daemonsets"}
		turboType = "workloadcontroller"
	case "Service":
		res = schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "services"}
		turboType = "service"
	case "Pod":
		res = schema.GroupVersionResource{
			Group:    "",
			Version:  "v1",
			Resource: "pods"}
		turboType = "pod"
	case "ReplicationController":
		res = schema.GroupVersionResource{
			Group:    util.K8sAPIReplicationControllerGV.Group,
			Version:  util.K8sAPIReplicationControllerGV.Version,
			Resource: util.ReplicationControllerResName}
		turboType = "workloadcontroller"
	case "Job.batch":
		res = schema.GroupVersionResource{
			Group:    "batch",
			Version:  "v1",
			Resource: "jobs"}
		turboType = "workloadcontroller"
	default:
		return nil, fmt.Errorf("unsupport group kind type %s", gk.String())
	}

	dynClient := p.ClusterScraper.(*cluster.ClusterScraper).DynamicClient
	resourceList, err := dynClient.Resource(res).Namespace(namespace).List(metav1.ListOptions{LabelSelector: labels.Set(selector.MatchLabels).String()})
	if err != nil {
		return nil, err
	}

	entities := []repository.AppEntity{}
	for _, r := range resourceList.Items {
		entity := repository.AppEntity{
			TurboType: turboType,
			Gvr:       res,
			Namespace: r.GetNamespace(),
			Name:      r.GetName(),
		}

		entities = append(entities, entity)
	}

	return entities, nil

}
