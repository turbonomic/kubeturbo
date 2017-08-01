package util

import (
	"encoding/json"
	"fmt"

	"github.com/golang/glog"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kclient "k8s.io/client-go/kubernetes"
	api "k8s.io/client-go/pkg/api/v1"
)

// get the scheduler name for ReplicaSet
func GetRSschedulerName(client *kclient.Clientset, nameSpace, name string) (string, error) {
	option := metav1.GetOptions{}
	if rs, err := client.ExtensionsV1beta1().ReplicaSets(nameSpace).Get(name, option); err == nil {
		return rs.Spec.Template.Spec.SchedulerName, nil
	} else {
		return "", err
	}
}

// get the scheduler name for ReplicationController
func GetRCschedulerName(client *kclient.Clientset, nameSpace, name string) (string, error) {
	option := metav1.GetOptions{}
	if rc, err := client.CoreV1().ReplicationControllers(nameSpace).Get(name, option); err == nil {
		return rc.Spec.Template.Spec.SchedulerName, nil
	} else {
		return "", err
	}
}

//update the schedulerName of a ReplicaSet to <schedulerName>
// return the previous schedulerName
func UpdateRSscheduler(client *kclient.Clientset, nameSpace, rsName, schedulerName string) (string, error) {
	currentName := ""

	rsClient := client.ExtensionsV1beta1().ReplicaSets(nameSpace)
	if rsClient == nil {
		return "", fmt.Errorf("failed to get ReplicaSet client in namespace: %v", nameSpace)
	}

	id := fmt.Sprintf("%v/%v", nameSpace, rsName)

	//1. get ReplicaSet
	option := metav1.GetOptions{}
	rs, err := rsClient.Get(rsName, option)
	if err != nil {
		err = fmt.Errorf("failed to get ReplicaSet-%v: %v", id, err)
		glog.Error(err)
		return currentName, err
	}

	if rs.Spec.Template.Spec.SchedulerName == schedulerName {
		glog.V(3).Infof("no need to update schedulerName for RS-[%v]", rsName)
		return "", nil
	}

	//2. update schedulerName
	rs.Spec.Template.Spec.SchedulerName = schedulerName
	_, err = rsClient.Update(rs)
	if err != nil {
		err = fmt.Errorf("failed to update RC-%v: %v", id, err)
		glog.Error(err)
		return currentName, err
	}

	return currentName, nil
}

//update the schedulerName of a ReplicationController
// if condName is not empty, then only current schedulerName is same to condName, then will do the update.
// return the previous schedulerName; or return "" if update failed.
func UpdateRCscheduler(client *kclient.Clientset, nameSpace, rcName, schedulerName string) (string, error) {
	currentName := ""

	id := fmt.Sprintf("%v/%v", nameSpace, rcName)
	rcClient := client.CoreV1().ReplicationControllers(nameSpace)

	//1. get
	option := metav1.GetOptions{}
	rc, err := rcClient.Get(rcName, option)
	if err != nil {
		err = fmt.Errorf("failed to get ReplicationController-%v: %v", id, err)
		glog.Error(err)
		return currentName, err
	}

	if rc.Spec.Template.Spec.SchedulerName == schedulerName {
		glog.V(3).Infof("no need to update schedulerName for RC-[%v]", rcName)
		return "", nil
	}

	//2. update
	rc.Spec.Template.Spec.SchedulerName = schedulerName
	_, err = rcClient.Update(rc)
	if err != nil {
		err = fmt.Errorf("failed to update RC-%v: %v", id, err)
		glog.Error(err)
		return currentName, err
	}

	return currentName, nil
}

//-------- for kclient version < 1.6 ------------------
// for Kubernetes version < 1.6, the schedulerName is set in Pod annotations, not in schedulerName field.
const (
	SchedulerAnnotationKey = "scheduler.alpha.kubernetes.io/name"
	emptyScheduler         = "None"
)

func parseAnnotatedScheduler(an map[string]string) (string, error) {
	if an == nil {
		return emptyScheduler, nil
	}

	result, ok := an[SchedulerAnnotationKey]
	if !ok || result == "" {
		return emptyScheduler, nil
	}

	return result, nil
}

func GetRSschedulerName15(client *kclient.Clientset, nameSpace, name string) (string, error) {
	option := metav1.GetOptions{}
	if rs, err := client.ExtensionsV1beta1().ReplicaSets(nameSpace).Get(name, option); err == nil {
		return parseAnnotatedScheduler(rs.Spec.Template.Annotations)
	} else {
		return "", err
	}
}

func GetRCschedulerName15(client *kclient.Clientset, nameSpace, name string) (string, error) {
	option := metav1.GetOptions{}
	if rc, err := client.CoreV1().ReplicationControllers(nameSpace).Get(name, option); err == nil {
		return parseAnnotatedScheduler(rc.Spec.Template.Annotations)
	} else {
		return "", err
	}
}

// shedulerName is set in pod.Annotations for kubernetes version < 1.6
func updateAnnotatedScheduler(pod *api.PodTemplateSpec, newName string) string {
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}

	defer func() {
		if len(pod.Annotations) == 0 {
			pod.Annotations = nil
		}
	}()

	if newName == emptyScheduler {
		current, ok := pod.Annotations[SchedulerAnnotationKey]
		if !ok {
			return emptyScheduler
		}

		delete(pod.Annotations, SchedulerAnnotationKey)
		return current
	}

	current, ok := pod.Annotations[SchedulerAnnotationKey]
	if !ok {
		pod.Annotations[SchedulerAnnotationKey] = newName
		return emptyScheduler
	}
	if current == "" {
		current = emptyScheduler
	}

	pod.Annotations[SchedulerAnnotationKey] = newName
	return current
}

//update the schedulerName of a ReplicationController, schedulerName is set in Sepc.Template.Annotations
// return the previous schedulerName, or return "" if update is not necessary or updated failed.
func UpdateRCscheduler15(client *kclient.Clientset, nameSpace, rcName, schedulerName string) (string, error) {
	currentName := ""

	id := fmt.Sprintf("%v/%v", nameSpace, rcName)
	rcClient := client.CoreV1().ReplicationControllers(nameSpace)
	if rcClient == nil {
		return "", fmt.Errorf("failed to get ReplicaSet client in namespace: %v", nameSpace)
	}

	//1. get
	option := metav1.GetOptions{}
	rc, err := rcClient.Get(rcName, option)
	if err != nil {
		err = fmt.Errorf("failed to get ReplicationController-%v: %v", id, err)
		glog.Error(err)
		return "", err
	}

	//2. update
	p := rc.Spec.Template
	currentName = updateAnnotatedScheduler(p, schedulerName)
	if currentName == schedulerName {
		return "", nil
	}

	_, err = rcClient.Update(rc)
	if err != nil {
		err = fmt.Errorf("failed to update RC-%v: %v.", id, err)
		glog.Error(err)
		return currentName, err
	}

	glog.V(2).Infof("update %v schedulerName [%v] to [%v]", id, currentName, schedulerName)

	return currentName, nil
}

//update the schedulerName of a ReplicaSet to <schedulerName>, schedulerName is set in Sepc.Template.Annotations
// return the previous schedulerName
func UpdateRSscheduler15(client *kclient.Clientset, nameSpace, rsName, schedulerName string) (string, error) {
	currentName := ""

	id := fmt.Sprintf("%v/%v", nameSpace, rsName)
	rsClient := client.ExtensionsV1beta1().ReplicaSets(nameSpace)
	if rsClient == nil {
		return "", fmt.Errorf("failed to get ReplicaSet client in namespace: %v", nameSpace)
	}

	//1. get ReplicaSet
	option := metav1.GetOptions{}
	rs, err := rsClient.Get(rsName, option)
	if err != nil {
		err = fmt.Errorf("failed to get ReplicaSet-%v: %v", id, err)
		glog.Error(err)
		return currentName, err
	}

	//2. update schedulerName
	p := &(rs.Spec.Template)
	currentName = updateAnnotatedScheduler(p, schedulerName)
	if currentName == schedulerName {
		return "", nil
	}

	_, err = rsClient.Update(rs)
	if err != nil {
		err = fmt.Errorf("failed to update RC-%v: %v.", id, err)
		glog.Error(err)
		return currentName, err
	}

	glog.V(2).Infof("update %v schedulerName [%v] to [%v]", id, currentName, schedulerName)

	return currentName, nil
}

//get kind and name of pod's parent Controller
func ParseParentInfo(pod *api.Pod) (string, string, error) {
	//1. check ownerReferences:
	if pod.OwnerReferences != nil && len(pod.OwnerReferences) > 0 {
		for _, owner := range pod.OwnerReferences {
			if *owner.Controller {
				return owner.Kind, owner.Name, nil
			}
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

func parsePodSchedulerName(pod *api.Pod, highver bool) string {

	if highver {
		return pod.Spec.SchedulerName
	}

	if pod.Annotations != nil {
		if sname, ok := pod.Annotations[SchedulerAnnotationKey]; ok {
			return sname
		}
	}

	return ""
}

// concate errors
func addErrors(prefix string, err1, err2 error) error {
	if err1 == nil && err2 == nil {
		return nil
	}

	rerr := fmt.Errorf("%v ", prefix)
	if err1 != nil {
		rerr = fmt.Errorf("%v %v", rerr, err1)
	}

	if err2 != nil {
		rerr = fmt.Errorf("%v %v", rerr, err2)
	}

	return rerr
}

//clean the Pods created by Controller while controller's scheduler is invalid.
func CleanPendingPod(client *kclient.Clientset, nameSpace, schedulerName, parentKind, parentName string, highver bool) error {
	podClient := client.CoreV1().Pods(nameSpace)

	option := metav1.ListOptions{
		FieldSelector: "status.phase=" + string(api.PodPending),
	}

	pods, err := podClient.List(option)
	if err != nil {
		glog.Errorf("failed to cleanPendingPod: %v", err)
		return err
	}

	var grace int64 = 0
	delOption := &metav1.DeleteOptions{GracePeriodSeconds: &grace}
	for i := range pods.Items {
		pod := &(pods.Items[i])

		//pod is being deleted
		if pod.DeletionGracePeriodSeconds != nil {
			continue
		}

		sname := parsePodSchedulerName(pod, highver)
		if sname != schedulerName {
			continue
		}

		kind, pname, err1 := ParseParentInfo(pod)
		if err1 != nil || pname == "" {
			continue
		}

		//clean all the pending Pod, not only for this operation.
		if parentKind != kind {
			//&& parentName != pname {
			continue
		}

		glog.V(3).Infof("Begin to delete Pending pod:%s/%s", nameSpace, pod.Name)
		err2 := podClient.Delete(pod.Name, delOption)
		if err2 != nil {
			glog.Warningf("failed ot delete pending pod:%s/%s: %v", nameSpace, pod.Name, err2)
			err = addErrors("", err, err2)
		}
	}

	return err
}
