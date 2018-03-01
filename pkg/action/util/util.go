package util

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/turbonomic/kubeturbo/pkg/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	client "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/apps/v1beta1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/dtofactory/property"

	"github.com/turbonomic/turbo-go-sdk/pkg/proto"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/labels"
	"strings"
)

const (
	maxDisplayNameLen        = 256
	osSccAnnotation          = "openshift.io/scc"
	supportedOsSccAnnotation = "restricted"
)

var (
	listOption = metav1.ListOptions{}
)

// Find RC based on pod labels.
// TODO. change this. Find rc based on its name and namespace or rc's UID.
func FindReplicationControllerForPod(kubeClient *client.Clientset, currentPod *api.Pod) (*api.ReplicationController, error) {
	// loop through all the labels in the pod and get List of RCs with selector that match at least one label
	podNamespace := currentPod.Namespace
	podName := currentPod.Name
	podLabels := currentPod.Labels

	if podLabels != nil {
		allRCs, err := GetAllReplicationControllers(kubeClient, podNamespace) // pod label is passed to list
		if err != nil {
			glog.Errorf("Error getting RCs")
			return nil, errors.New("Error  getting RC list")
		}
		rc, err := findRCBasedOnPodLabel(allRCs, podLabels)
		if err != nil {
			return nil, fmt.Errorf("Failed to find RC for Pod %s/%s: %s", podNamespace, podName, err)
		}
		return rc, nil

	} else {
		glog.Warningf("Pod %s/%s has no label. There is no RC for the Pod.", podNamespace, podName)
	}
	return nil, nil
}

// Get all replication controllers defined in the specified namespace.
func GetAllDeployments(kubeClient *client.Clientset, namespace string) ([]v1beta1.Deployment, error) {
	deploymentList, err := kubeClient.AppsV1beta1().Deployments(namespace).List(listOption)
	if err != nil {
		return nil, fmt.Errorf("Error when getting all the deployments: %s", err)
	}
	return deploymentList.Items, nil
}

// TODO. change this. Find deployment based on its name and namespace or UID.
func findDeploymentBasedOnPodLabel(deploymentsList []v1beta1.Deployment, labels map[string]string) (*v1beta1.Deployment, error) {
	for _, deployment := range deploymentsList {
		findDeployment := true
		// check if a Deployment controls pods with given labels
		for key, val := range deployment.Spec.Selector.MatchLabels {
			if labels[key] == "" || labels[key] != val {
				findDeployment = false
				break
			}
		}
		if findDeployment {
			return &deployment, nil
		}
	}
	return nil, errors.New("No Deployment has selectors match Pod labels.")
}

func FindDeploymentForPod(kubeClient *client.Clientset, currentPod *api.Pod) (*v1beta1.Deployment, error) {
	// loop through all the labels in the pod and get List of RCs with selector that match at least one label
	podNamespace := currentPod.Namespace
	podName := currentPod.Name
	podLabels := currentPod.Labels

	if podLabels != nil {
		allDeployments, err := GetAllDeployments(kubeClient, podNamespace) // pod label is passed to list
		if err != nil {
			glog.Errorf("Error getting RCs")
			return nil, errors.New("Error  getting Deployment list")
		}
		rc, err := findDeploymentBasedOnPodLabel(allDeployments, podLabels)
		if err != nil {
			return nil, fmt.Errorf("Failed to find Deployment for Pod %s/%s: %s", podNamespace, podName, err)
		}
		return rc, nil

	} else {
		glog.Warningf("Pod %s/%s has no label. There is no Deployment for the Pod.", podNamespace, podName)
	}
	return nil, nil
}

// Get all replication controllers defined in the specified namespace.
func GetAllReplicationControllers(kubeClient *client.Clientset, namespace string) ([]api.ReplicationController, error) {
	rcList, err := kubeClient.CoreV1().ReplicationControllers(namespace).List(listOption)
	if err != nil {
		return nil, fmt.Errorf("Error when getting all the replication controllers: %s", err)
	}
	return rcList.Items, nil
}

func findRCBasedOnPodLabel(rcList []api.ReplicationController, labels map[string]string) (*api.ReplicationController, error) {
	for _, rc := range rcList {
		findRC := true
		// check if a RC controlls pods with given labels
		for key, val := range rc.Spec.Selector {
			if labels[key] == "" || labels[key] != val {
				findRC = false
				break
			}
		}
		if findRC {
			return &rc, nil
		}
	}
	return nil, errors.New("No RC has selectors match Pod labels.")
}

// Get all nodes currently in K8s.
func GetAllNodes(kubeClient *client.Clientset) ([]api.Node, error) {
	nodeList, err := kubeClient.CoreV1().Nodes().List(listOption)
	if err != nil {
		return nil, fmt.Errorf("Error when getting all the nodes :%s", err)
	}
	return nodeList.Items, nil
}

// Iterate all nodes to find the name of the node which has the provided IP address.
// TODO. We can also create a IP->NodeName map to save time. But it consumes space.
func GetNodebyIP(kubeClient *client.Clientset, machineIPs []string) (*api.Node, error) {
	ipAddresses := machineIPs
	allNodes, err := GetAllNodes(kubeClient)
	if err != nil {
		return nil, err
	}
	for i := range allNodes {
		node := &allNodes[i]
		nodeAddresses := node.Status.Addresses
		for _, nodeAddress := range nodeAddresses {
			for _, machineIP := range ipAddresses {
				if nodeAddress.Address == machineIP {
					return node, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("Cannot find node with IPs %s", ipAddresses)
}

// try to get k8s.node by nodeName
func GetNodebyName(kubeClient *client.Clientset, nodeName string) (*api.Node, error) {
	node, err := kubeClient.CoreV1().Nodes().Get(nodeName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Failed to get node(%v): %v", nodeName, err)
		return nil, err
	}

	return node, nil
}

// Iterate all nodes to find the name of the node which has the provided IP address.
func GetNodebyUUID(kubeClient *client.Clientset, uuid string) (*api.Node, error) {
	allNodes, err := GetAllNodes(kubeClient)
	if err != nil {
		glog.Errorf("Failed to list nodes: %v", err)
		return nil, err
	}

	for i := range allNodes {
		node := &allNodes[i]
		if strings.EqualFold(uuid, node.Status.NodeInfo.SystemUUID) {
			return node, nil
		}
	}

	return nil, fmt.Errorf("Cannot find node with UUID %s", uuid)
}

func GetNodeFromProperties(kubeClient *client.Clientset, properties []*proto.EntityDTO_EntityProperty) (*api.Node, error) {
	name := property.GetNodeNameFromProperty(properties)
	if len(name) < 1 {
		err := fmt.Errorf("cannot found node name from properites: %++v", properties)
		glog.Error(err)
		return nil, err
	}

	return GetNodebyName(kubeClient, name)
}

// Get a pod based on received entity properties.
func GetPodFromProperties(kubeClient *client.Clientset, entityType proto.EntityDTO_EntityType,
	properties []*proto.EntityDTO_EntityProperty) (*api.Pod, error) {
	var podNamespace, podName string
	switch entityType {
	case proto.EntityDTO_APPLICATION:
		podNamespace, podName, _ = property.GetHostingPodInfoFromProperty(properties)
	case proto.EntityDTO_CONTAINER_POD:
		podNamespace, podName, _ = property.GetPodInfoFromProperty(properties)
	default:
		return nil, fmt.Errorf("cannot find pod based on properties of an entity with type: %s", entityType)
	}
	if podNamespace == "" || podName == "" {
		return nil, fmt.Errorf("railed to find  pod info from pod properties: %v", properties)
	}
	return kubeClient.CoreV1().Pods(podNamespace).Get(podName, metav1.GetOptions{})
}

// Get a pod instance from the uuid of a pod. Since there is no support for uuid lookup, we have to get all the pods
// and then find the correct pod based on uuid match.
func GetPodFromUUID(kubeClient *client.Clientset, podUUID string) (*api.Pod, error) {
	namespace := api.NamespaceAll
	podList, err := kubeClient.CoreV1().Pods(namespace).List(listOption)
	if err != nil {
		return nil, fmt.Errorf("error getting all the desired pods from Kubernetes cluster: %s", err)
	}
	for _, pod := range podList.Items {
		if string(pod.UID) == podUUID {
			return &pod, nil
		}
	}
	return nil, fmt.Errorf("cannot find pod based on given uuid: %s", podUUID)
}

// Get k8s.Pod through OpsMgr.ServiceEntity.DisplayName (Entity can be ContainerPod or Container)
//      if serviceEntity is a containerPod, then displayName is "namespace/podName";
//      if serviceEntity is a container, then displayName is "namespace/podName/containerName";
// uuid is the pod's UUID
// Note: displayName for application is "App-namespace/podName", the pods info can be got by the provider's displayname
func GetPodFromDisplayNameOrUUID(kclient *client.Clientset, displayname, uuid string) (*api.Pod, error) {
	if pod, err := getaPodFromDisplayName(kclient, displayname, uuid); err == nil {
		glog.V(3).Infof("Get pod(%s) from displayName.", displayname)
		return pod, err
	}

	return GetPodFromUUID(kclient, uuid)
}

func getaPodFromDisplayName(kclient *client.Clientset, displayname, uuid string) (*api.Pod, error) {
	sep := "/"
	items := strings.Split(displayname, sep)
	if len(items) < 2 {
		err := fmt.Errorf("Cannot get namespace/podname from %v", displayname)
		glog.Error(err.Error())
		return nil, err
	}

	//1. get Namespace
	namespace := strings.TrimSpace(items[0])
	if len(namespace) < 1 {
		err := fmt.Errorf("Parsed namespace is empty from %v", displayname)
		glog.Errorf(err.Error())
		return nil, err
	}

	//2. get PodName
	name := strings.TrimSpace(items[1])
	if len(name) < 1 {
		err := fmt.Errorf("Parsed name is empty from %v", displayname)
		glog.Errorf(err.Error())
		return nil, err
	}

	//3. get Pod
	pod, err := GetPod(kclient, namespace, name)
	if err != nil {
		err = fmt.Errorf("Failed to get Pod by %v/%v: %v", namespace, name, err)
		glog.Errorf(err.Error())
		return nil, err
	}

	//4. if displayName is short, or it contains part of the containerName
	if len(displayname) < maxDisplayNameLen || len(items) > 2 {
		return pod, nil
	}
	// otherwise check the pod's UID
	if string(pod.UID) == uuid {
		return pod, nil
	}

	return nil, fmt.Errorf("Failed getting pod from DisplayName %s", displayname)
}

// Find which pod is the app running based on the received action request.
func FindApplicationPodProvider(kubeClient *client.Clientset, providers []*proto.ActionItemDTO_ProviderInfo) (*api.Pod, error) {
	if providers == nil || len(providers) < 1 {
		return nil, errors.New("Cannot find any provider.")
	}

	for _, providerInfo := range providers {
		if providerInfo == nil {
			continue
		}
		if providerInfo.GetEntityType() == proto.EntityDTO_CONTAINER_POD {
			providerIDs := providerInfo.GetIds()
			for _, id := range providerIDs {
				podProvider, err := GetPodFromUUID(kubeClient, id)
				if err != nil {
					glog.Errorf("Error getting pod provider from pod identifier %s", id)
					continue
				} else {
					return podProvider, nil
				}
			}
		}
	}
	return nil, errors.New("Cannot find any Pod provider")
}

// Given namespace and name, return an identifier in the format, namespace/name
func BuildIdentifier(namespace, name string) string {
	return namespace + "/" + name
}

func GetPod(kubeClient *client.Clientset, namespace, name string) (*api.Pod, error) {
	return kubeClient.CoreV1().Pods(namespace).Get(name, metav1.GetOptions{})
}

func CreatePod(kubeClient *client.Clientset, pod *api.Pod) (*api.Pod, error) {
	return kubeClient.CoreV1().Pods(pod.Namespace).Create(pod)
}

func DeletePod(kubeClient *client.Clientset, namespace, name string, grace int64) error {
	opt := &metav1.DeleteOptions{GracePeriodSeconds: &grace}
	return kubeClient.CoreV1().Pods(namespace).Delete(name, opt)
}

func parseOwnerReferences(owners []metav1.OwnerReference) (string, string) {
	for i := range owners {
		owner := &owners[i]
		if *(owner.Controller) && len(owner.Kind) > 0 && len(owner.Name) > 0 {
			return owner.Kind, owner.Name
		}
	}

	return "", ""
}

func GetPodParentInfo(pod *api.Pod) (string, string, error) {
	//1. check ownerReferences:

	if pod.OwnerReferences != nil && len(pod.OwnerReferences) > 0 {
		kind, name := parseOwnerReferences(pod.OwnerReferences)
		if len(kind) > 0 && len(name) > 0 {
			return kind, name, nil
		}
	}

	glog.V(4).Infof("no parent-info for pod-%v/%v in OwnerReferences.", pod.Namespace, pod.Name)

	//2. check annotations:
	if pod.Annotations != nil && len(pod.Annotations) > 0 {
		key := "kubernetes.io/created-by"
		if value, ok := pod.Annotations[key]; ok {

			var ref api.SerializedReference

			if err := json.Unmarshal([]byte(value), &ref); err != nil {
				err = fmt.Errorf("failed to decode parent annoation:%v", err)
				glog.Errorf("%v\n%v", err, value)
				return "", "", err
			}

			return ref.Reference.Kind, ref.Reference.Name, nil
		}
	}

	glog.V(4).Infof("no parent-info for pod-%v/%v in Annotations.", pod.Namespace, pod.Name)

	return "", "", nil
}

// get grandParent(parent's parent) information of a pod: kind, name
// If parent does not have parent, then return parent info.
// Note: if parent kind is "ReplicaSet", then its parent's parent can be a "Deployment"
func GetPodGrandInfo(kclient *client.Clientset, pod *api.Pod) (string, string, error) {
	//1. get Parent info: kind and name;
	kind, name, err := GetPodParentInfo(pod)
	if err != nil {
		return "", "", err
	}

	//2. if parent is "ReplicaSet", check parent's parent
	if strings.EqualFold(kind, "ReplicaSet") {
		//2.1 get parent object
		rs, err := kclient.ExtensionsV1beta1().ReplicaSets(pod.Namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			err = fmt.Errorf("Failed to get ReplicaSet[%v/%v]: %v", pod.Namespace, name, err)
			glog.Error(err.Error())
			return "", "", err
		}

		//2.2 get parent's parent info by parsing ownerReferences:
		// TODO: The ownerReferences of ReplicaSet is supported only in 1.6.0 and afetr
		if rs.OwnerReferences != nil && len(rs.OwnerReferences) > 0 {
			gkind, gname := parseOwnerReferences(rs.OwnerReferences)
			if len(gkind) > 0 && len(gname) > 0 {
				return gkind, gname, nil
			}
		}
	}

	return kind, name, nil
}

// Updates replica of a controller.
// Currently, it supports replication controllers, replica sets, and deployments
func UpdateReplicas(kubeClient *client.Clientset, namespace, contName, contKind string, newReplicas int) error {
	newValue := int32(newReplicas)
	switch contKind {
	case util.KindReplicationController:
		cont, err := kubeClient.CoreV1().ReplicationControllers(namespace).Get(contName, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("Get %s failed: %v", contKind, err)
			return err
		}
		cont.Spec.Replicas = &newValue

		_, err = kubeClient.CoreV1().ReplicationControllers(namespace).Update(cont)
		return err
	case util.KindReplicaSet:
		cont, err := kubeClient.ExtensionsV1beta1().ReplicaSets(namespace).Get(contName, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("Get %s failed: %v", contKind, err)
			return err
		}
		cont.Spec.Replicas = &newValue

		_, err = kubeClient.ExtensionsV1beta1().ReplicaSets(namespace).Update(cont)
		return err
	case util.KindDeployment:
		cont, err := kubeClient.ExtensionsV1beta1().Deployments(namespace).Get(contName, metav1.GetOptions{})
		if err != nil {
			glog.Errorf("Get %s failed: %v", contKind, err)
			return err
		}
		cont.Spec.Replicas = &newValue

		_, err = kubeClient.ExtensionsV1beta1().Deployments(namespace).Update(cont)
		return err
	default:
		return fmt.Errorf("unsupported kind: %s", contKind)
	}
}

// Gets status of replicas and ready replicas of a controller.
// Currently, it supports replication controllers, replica sets, and deployments
func GetReplicaStatus(kubeClient *client.Clientset, namespace, contName, contKind string) (int, int, error) {
	switch contKind {
	case util.KindReplicationController:
		if cont, err := kubeClient.CoreV1().ReplicationControllers(namespace).Get(contName, metav1.GetOptions{}); err != nil {
			glog.Errorf("Get %s failed: %v", contKind, err)
			return 0, 0, err
		} else {
			return int(cont.Status.Replicas), int(cont.Status.ReadyReplicas), nil
		}
	case util.KindReplicaSet:
		if cont, err := kubeClient.ExtensionsV1beta1().ReplicaSets(namespace).Get(contName, metav1.GetOptions{}); err != nil {
			glog.Errorf("Get %s failed: %v", contKind, err)
			return 0, 0, err
		} else {
			return int(cont.Status.Replicas), int(cont.Status.ReadyReplicas), nil
		}
	case util.KindDeployment:
		if cont, err := kubeClient.ExtensionsV1beta1().Deployments(namespace).Get(contName, metav1.GetOptions{}); err != nil {
			glog.Errorf("Get %s failed: %v", contKind, err)
			return 0, 0, err
		} else {
			return int(cont.Status.Replicas), int(cont.Status.ReadyReplicas), nil
		}
	default:
		return 0, 0, fmt.Errorf("unsupported kind: %s", contKind)
	}
}

func getSelectorFromController(kubeClient *client.Clientset, namespace, contName, contKind string) (map[string]string, error) {
	switch contKind {
	case util.KindReplicationController:
		if cont, err := kubeClient.CoreV1().ReplicationControllers(namespace).Get(contName, metav1.GetOptions{}); err != nil {
			glog.Errorf("Get %s failed: %v", contKind, err)
			return nil, err
		} else {
			return cont.Spec.Selector, nil
		}
	case util.KindReplicaSet:
		if cont, err := kubeClient.ExtensionsV1beta1().ReplicaSets(namespace).Get(contName, metav1.GetOptions{}); err != nil {
			glog.Errorf("Get %s failed: %v", contKind, err)
			return nil, err
		} else {
			return cont.Spec.Selector.MatchLabels, nil
		}
	case util.KindDeployment:
		if cont, err := kubeClient.ExtensionsV1beta1().Deployments(namespace).Get(contName, metav1.GetOptions{}); err != nil {
			glog.Errorf("Get %s failed: %v", contKind, err)
			return nil, err
		} else {
			return cont.Spec.Selector.MatchLabels, nil
		}
	default:
		return nil, fmt.Errorf("unsupported kind: %s", contKind)
	}
}

// Finds all child pods of a controller.
// Currently, it supports replication controllers, replica sets, and deployments
func FindPodsByController(kubeClient *client.Clientset, namespace, contName, contKind string) ([]*api.Pod, error) {
	selector, err := getSelectorFromController(kubeClient, namespace, contName, contKind)
	if err != nil {
		return nil, err
	}

	// Finds all pods with the labels of the controller
	podList, err := kubeClient.CoreV1().Pods(namespace).List(metav1.ListOptions{
		LabelSelector: labels.Set(selector).AsSelector().String(),
	})
	if err != nil {
		return nil, fmt.Errorf("error getting all pods by %s %s/%s: %s", contKind, namespace, contName, err)
	}

	pods := []*api.Pod{}

	for i := range podList.Items {
		pod := &(podList.Items[i])
		_, name, err := GetPodGrandInfo(kubeClient, pod)

		if err != nil {
			continue
		}
		if name == contName {
			pods = append(pods, pod)
		}
	}
	return pods, nil
}

// check whether parentKind is supported for MovePod/ResizeContainer actions
// currently, these actions can only works on barePod, ReplicaSet, and ReplicationController
//   Note: pod's parent cannot be Deployment. Deployment will create/control ReplicaSet, and ReplicaSet will create/control Pods.
func SupportedParent(parentKind string) bool {
	if parentKind == "" {
		return true
	}

	if strings.EqualFold(parentKind, util.KindReplicaSet) {
		return true
	}

	if strings.EqualFold(parentKind, util.KindReplicationController) {
		return true
	}

	return false
}

func AddAnnotation(pod *api.Pod, key, value string) {
	annotations := pod.Annotations
	if annotations == nil {
		annotations = make(map[string]string)
		pod.Annotations = annotations
	}

	if oldv, ok := annotations[key]; ok {
		glog.Warningf("Overwrite pod(%v/%v)'s annotation (%v=%v)", pod.Namespace, pod.Name, key, oldv)
	}
	annotations[key] = value
	return
}

// SupportPrivilegePod checks the pod annotations and returns true if the priviledge requirement
// is supported. Currently, it only matters for Openshift clusters as it checks the annotation of
// Openshift SCC (openshift.io/scc). Kubeturbo supports the restricted (default) SCC and rejects
// other non-empty SCC's if the annotation is set.
// See details on https://docs.openshift.org/latest/admin_guide/manage_scc.html.
func SupportPrivilegePod(pod *api.Pod) bool {
	annotations := pod.Annotations
	key := osSccAnnotation
	value := supportedOsSccAnnotation
	if annotations != nil && annotations[key] != "" && annotations[key] != value {
		glog.Warningf("The pod %s has unsupported privilege %s", pod.Name, annotations[key])
		return false
	}

	return true
}
