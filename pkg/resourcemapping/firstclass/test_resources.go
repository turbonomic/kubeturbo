package firstclass

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	_testNamespace_ = "default"

	_testDeploymentGVR_ = schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments",
	}

	_testDeploymentName_               = "test-deploy"
	_testDeploymentContainerName_      = "test-deploy-container"
	_testDeploymentContainerImageName_ = "test-deploy-container-image"
	_testDeploySelectorLabels          = map[string]string{"key": "value"}
	_testDeployment_                   = &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      _testDeploymentName_,
			Namespace: _testNamespace_,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: _testDeploySelectorLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: _testDeploySelectorLabels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  _testDeploymentContainerName_,
							Image: _testDeploymentContainerImageName_,
						},
					},
				},
			},
		},
	}
)
