package worker

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	client "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/master"
	"github.com/turbonomic/kubeturbo/pkg/discovery/probe"
	"github.com/turbonomic/kubeturbo/pkg/discovery/task"
	"github.com/turbonomic/kubeturbo/pkg/discovery/util"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
)

const (
	labelSelectEverything = ""
	fieldSelectEverything = ""

	k8sSvcDiscWorkerID string = "ServiceDiscoveryWorker"
)

type k8sServiceDiscoveryWorkerConfig struct {
	// TODO, once we know what are the required method, we can use a clusterAccessor.
	kubeClient *client.Clientset
}

func NewK8sServiceDiscoveryWorkerConfig(kubeClient *client.Clientset) *k8sServiceDiscoveryWorkerConfig {
	return &k8sServiceDiscoveryWorkerConfig{
		kubeClient: kubeClient,
	}
}

type k8sServiceDiscoveryWorker struct {
	id string

	// TODO, once we know what are the required method, we can use a clusterAccessor.
	config *k8sServiceDiscoveryWorkerConfig

	// Cluster ID for the current Kubernetes cluster.
	clusterID string

	// Currently a worker only receives one task at a time.
	task *task.Task

	podClusterIDToPodMap map[string]*api.Pod
}

func NewK8sServiceDiscoveryWorker(config *k8sServiceDiscoveryWorkerConfig) (*k8sServiceDiscoveryWorker, error) {
	clusterID, err := master.GetKubernetesServiceID(config.kubeClient)
	if err != nil {
		return nil, fmt.Errorf("failed to get current Kubernetes cluster ID: %s", err)
	}

	return &k8sServiceDiscoveryWorker{
		id:        k8sSvcDiscWorkerID,
		config:    config,
		clusterID: clusterID,
	}, nil
}

func (svcDiscWorker *k8sServiceDiscoveryWorker) ReceiveTask(task *task.Task) error {
	if svcDiscWorker.task != nil {
		return fmt.Errorf("The current worker %s has already been assigned a task and has not finished yet", svcDiscWorker.id)
	}
	svcDiscWorker.task = task
	return nil
}

// post-process the entityDTOs and create service entityDTOs.
func (svcDiscWorker *k8sServiceDiscoveryWorker) Do(entityDTOs []*proto.EntityDTO) *task.TaskResult {

	applicationDTOs := getAllApplicationEntityDTOs(entityDTOs)

	svcDiscoveryResult, err := svcDiscWorker.parseService(applicationDTOs)
	if err != nil {
		return task.NewTaskResult(svcDiscWorker.id, task.TaskFailed).WithErr(err)
	}
	glog.V(3).Infof("Service discovery result is: %++v", svcDiscoveryResult)
	result := task.NewTaskResult(svcDiscWorker.id, task.TaskSucceeded).WithContent(svcDiscoveryResult)
	return result
}

func getAllApplicationEntityDTOs(entityDTOs []*proto.EntityDTO) map[string]*proto.EntityDTO {
	appEntityDTOsMap := make(map[string]*proto.EntityDTO)
	for _, e := range entityDTOs {
		if e.GetEntityType() == proto.EntityDTO_APPLICATION {
			appEntityDTOsMap[e.GetId()] = e
		}
	}
	return appEntityDTOsMap
}

// Parse Services inside Kubernetes and build entityDTO as VApp.
func (svcDiscWorker *k8sServiceDiscoveryWorker) parseService(appDTOs map[string]*proto.EntityDTO) ([]*proto.EntityDTO, error) {

	serviceList, err := getAllServices(svcDiscWorker.config.kubeClient)
	if err != nil {
		return nil, err
	}
	endpointList, err := getAllEndpoints(svcDiscWorker.config.kubeClient)
	if err != nil {
		return nil, err
	}

	podClusterIDToPodMap, err := svcDiscWorker.buildPodClusterIDToPod()
	if err != nil {
		return nil, fmt.Errorf("failed to index pods in current cluster: %s", err)
	}

	svcPodMap := groupPodsAndServices(serviceList, endpointList, podClusterIDToPodMap)

	svcEntityDTOBuilder := &dtofactory.ServiceEntityDTOBuilder{}
	svcEntityDTOs, err := svcEntityDTOBuilder.BuildSvcEntityDTO(svcPodMap, svcDiscWorker.clusterID, appDTOs)
	if err != nil {
		return nil, fmt.Errorf("Error while creating service entityDTOs: %v", err)
	}

	return svcEntityDTOs, nil
}

func groupPodsAndServices(serviceList []*api.Service, endpointList []*api.Endpoints, podIDMap map[string]*api.Pod) map[*api.Service][]*api.Pod {

	// first make a endpoint map, key is endpoints cluster ID; value is endpoint object
	endpointMap := make(map[string]*api.Endpoints)
	for _, endpoint := range endpointList {
		endpointClusterID := util.GetEndpointsClusterID(endpoint)
		endpointMap[endpointClusterID] = endpoint
	}

	svcPodMap := make(map[*api.Service][]*api.Pod)
	for _, service := range serviceList {
		serviceClusterID := util.GetServiceClusterID(service)
		podClusterIDs := findPodEndpoints(service, endpointMap)
		if len(podClusterIDs) < 1 {
			glog.V(3).Infof("%s is a standalone service without any enpoint pod.", serviceClusterID)
			continue
		}
		glog.V(4).Infof("service %s has the following pod as endpoints %v", serviceClusterID, podClusterIDs)

		podList := []*api.Pod{}
		for podClusterID := range podClusterIDs {
			// find the pod
			pod, found := podIDMap[podClusterID]
			if !found {
				glog.Warningf("Cannot find %s in current cluster", podClusterID)
				continue
			}
			podList = append(podList, pod)
		}
		svcPodMap[service] = podList
	}
	return svcPodMap
}

// For every service, find the pods for this service.
func findPodEndpoints(service *api.Service, endpointMap map[string]*api.Endpoints) map[string]struct{} {
	serviceClusterID := util.GetServiceClusterID(service)
	serviceEndpoint := endpointMap[serviceClusterID]
	if serviceEndpoint == nil {
		return nil
	}
	subsets := serviceEndpoint.Subsets
	podClusterIDSet := make(map[string]struct{})
	for _, endpointSubset := range subsets {
		addresses := endpointSubset.Addresses
		for _, address := range addresses {
			target := address.TargetRef
			if target == nil {
				continue
			}
			podName := target.Name
			podNamespace := target.Namespace
			podClusterID := probe.GetPodClusterID(podNamespace, podName)
			// get the pod name and the service name
			podClusterIDSet[podClusterID] = struct{}{}
		}
	}
	return podClusterIDSet
}

// Index pod based on pod's clusterID.
func (svcDiscWorker *k8sServiceDiscoveryWorker) buildPodClusterIDToPod() (map[string]*api.Pod, error) {
	listOption := &metav1.ListOptions{
		LabelSelector: labelSelectEverything,
		FieldSelector: fieldSelectEverything,
	}
	podList, err := svcDiscWorker.config.kubeClient.Pods(api.NamespaceAll).List(*listOption)
	if err != nil {
		return nil, fmt.Errorf("failed to get all running pods in Kubernetes cluster: %s", err)
	}
	podMap := make(map[string]*api.Pod)
	for _, p := range podList.Items {
		pod := p
		podMap[probe.GetPodClusterID(pod.Namespace, pod.Name)] = &pod
	}
	return podMap, nil
}

func getAllServices(kubeClient *client.Clientset) ([]*api.Service, error) {
	listOption := metav1.ListOptions{
		LabelSelector: labels.Everything().String(),
	}
	serviceList, err := kubeClient.CoreV1().Services(api.NamespaceAll).List(listOption)
	if err != nil {
		return nil, fmt.Errorf("Error listing services: %s", err)
	}

	var serviceItems []*api.Service
	for _, service := range serviceList.Items {
		s := service
		serviceItems = append(serviceItems, &s)
	}

	glog.V(2).Infof("Discovering Services, now the cluster has %d services", len(serviceItems))

	return serviceItems, nil
}

func getAllEndpoints(kubeClient *client.Clientset) ([]*api.Endpoints, error) {
	listOption := metav1.ListOptions{
		LabelSelector: labels.Everything().String(),
	}
	epList, err := kubeClient.CoreV1().Endpoints(api.NamespaceAll).List(listOption)
	if err != nil {
		return nil, fmt.Errorf("Error listing endpoints: %s", err)
	}

	var epItems []*api.Endpoints
	for _, endpoint := range epList.Items {
		ep := endpoint
		epItems = append(epItems, &ep)
	}

	return epItems, nil
}
