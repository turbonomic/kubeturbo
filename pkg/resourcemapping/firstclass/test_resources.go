package firstclass

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									_testResourceName_: _testQuantityOne_,
								},
								Limits: corev1.ResourceList{
									_testResourceName_: _testQuantityTwo_,
								},
							},
						},
					},
				},
			},
		},
	}

	_testResourceName_  = corev1.ResourceName("cpu")
	_testQuantityOne_   = resource.MustParse("1")
	_testQuantityTwo_   = resource.MustParse("2")
	_testQuantityEight_ = resource.MustParse("8")

	_testNodeLableKeyHostName_ = "hostname"
	_testNodeName1_            = "name1"
	_testNode1_                = &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: _testNodeName1_,
			Labels: map[string]string{
				_testNodeLableKeyHostName_: _testNodeName1_,
			},
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				_testResourceName_: _testQuantityEight_,
			},
		},
	}

	_testNodeName2_ = "name2"
	_testNode2_     = &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: _testNodeName2_,
			Labels: map[string]string{
				_testNodeLableKeyHostName_: _testNodeName2_,
			},
		},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				_testResourceName_: _testQuantityEight_,
			},
		},
	}

	_testQuotaName_         = "quota"
	_testQuotaResourceName_ = corev1.ResourceName(prefixRequests) + _testResourceName_
	_testNamespaceQuota_    = corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      _testQuotaName_,
			Namespace: _testNamespace_,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				_testQuotaResourceName_: _testQuantityEight_,
			},
		},
		Status: corev1.ResourceQuotaStatus{
			Hard: corev1.ResourceList{
				_testQuotaResourceName_: _testQuantityEight_,
			},
			Used: corev1.ResourceList{
				_testQuotaResourceName_: _testQuantityTwo_,
			},
		},
	}
)
