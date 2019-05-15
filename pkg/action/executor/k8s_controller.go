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
// Note: Use pointer for in-place update
type k8sControllerSpec struct {
	replicas *int32
}

// ReplicationController
type replicationController struct {
	k8sController
	client typedcorev1.ReplicationControllerInterface
	rc     *apicorev1.ReplicationController
}

func (rc *replicationController) get(name string) (*k8sControllerSpec, error) {
	var err error
	rc.rc, err = rc.client.Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &k8sControllerSpec{
		replicas: rc.rc.Spec.Replicas,
	}, nil
}

func (rc *replicationController) update() error {
	if _, err := rc.client.Update(rc.rc); err != nil {
		return err
	}
	return nil
}

func (rc *replicationController) String() string {
	return "ReplicationController"
}

// ReplicaSet
type replicaSet struct {
	k8sController
	client typedextv1beta1.ReplicaSetInterface
	rs     *apiextv1beta1.ReplicaSet
}

func (rs *replicaSet) get(name string) (*k8sControllerSpec, error) {
	var err error
	rs.rs, err = rs.client.Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &k8sControllerSpec{
		replicas: rs.rs.Spec.Replicas,
	}, nil
}

func (rs *replicaSet) update() error {
	if _, err := rs.client.Update(rs.rs); err != nil {
		return err
	}
	return nil
}

func (rs *replicaSet) String() string {
	return "ReplicaSet"
}

// Deployment
type deployment struct {
	k8sController
	client typedappsv1beta1.DeploymentInterface
	dep    *apiappsv1beta1.Deployment
}

func (dep *deployment) get(name string) (*k8sControllerSpec, error) {
	var err error
	dep.dep, err = dep.client.Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &k8sControllerSpec{
		replicas: dep.dep.Spec.Replicas,
	}, nil
}

func (dep *deployment) update() error {
	if _, err := dep.client.Update(dep.dep); err != nil {
		return err
	}
	return nil
}

func (dep *deployment) String() string {
	return "Deployment"
}
