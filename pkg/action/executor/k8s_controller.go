package executor

import (
	apiappsv1beta1 "k8s.io/api/apps/v1beta1"
	apicorev1 "k8s.io/api/core/v1"
	apiextv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	typedappsv1beta1 "k8s.io/client-go/kubernetes/typed/apps/v1beta1"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	typedextv1beta1 "k8s.io/client-go/kubernetes/typed/extensions/v1beta1"
)

// k8sController defines a common interface for kubernetes controller actions
// Currently supported controllers include:
// - ReplicationController
// - ReplicaSet
// - Deployment
type k8sController interface {
	get(name string) (*k8sControllerSpec, error)
	update() error
}

// k8sControllerSpec defines a set of objects that we want to update:
// - replicas: The replicas of a controller to update for horizontal scale
// - podSpec: The pod template of a controller to update for consistent resize
// Note: Use pointer for in-place update
type k8sControllerSpec struct {
	replicas *int32
	podSpec  *apicorev1.PodSpec
}

// replicationController represents the k8s ReplicationController resource
type replicationController struct {
	k8sController
	client typedcorev1.ReplicationControllerInterface
	rc     *apicorev1.ReplicationController
}

// get takes the name of the replicationcontroller,
// returns and saves the corresponding replicationcontroller object from the server
func (rc *replicationController) get(name string) (*k8sControllerSpec, error) {
	var err error
	rc.rc, err = rc.client.Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &k8sControllerSpec{
		replicas: rc.rc.Spec.Replicas,
		podSpec:  &rc.rc.Spec.Template.Spec,
	}, nil
}

// update takes the saved replicationcontroller object and updates it with the server
func (rc *replicationController) update() error {
	_, err := rc.client.Update(rc.rc)
	return err
}

func (rc *replicationController) String() string {
	return "ReplicationController"
}

// replicaSet represents the k8s ReplicaSet resource
type replicaSet struct {
	k8sController
	client typedextv1beta1.ReplicaSetInterface
	rs     *apiextv1beta1.ReplicaSet
}

// get takes the name of the replicaset,
// returns and saves the corresponding replicaset object from the server
func (rs *replicaSet) get(name string) (*k8sControllerSpec, error) {
	var err error
	rs.rs, err = rs.client.Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &k8sControllerSpec{
		replicas: rs.rs.Spec.Replicas,
		podSpec:  &rs.rs.Spec.Template.Spec,
	}, nil
}

// update takes the saved replicaset object and updates it with the server
func (rs *replicaSet) update() error {
	_, err := rs.client.Update(rs.rs)
	return err
}

func (rs *replicaSet) String() string {
	return "ReplicaSet"
}

// deployment represents the k8s Deployment resource
type deployment struct {
	k8sController
	client typedappsv1beta1.DeploymentInterface
	dep    *apiappsv1beta1.Deployment
}

// get takes the name of the deployment,
// returns and saves the corresponding deployment object from the server
func (dep *deployment) get(name string) (*k8sControllerSpec, error) {
	var err error
	dep.dep, err = dep.client.Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &k8sControllerSpec{
		replicas: dep.dep.Spec.Replicas,
		podSpec:  &dep.dep.Spec.Template.Spec,
	}, nil
}

// update takes the saved deployment object and updates it with the server
func (dep *deployment) update() error {
	_, err := dep.client.Update(dep.dep)
	return err
}

func (dep *deployment) String() string {
	return "Deployment"
}
