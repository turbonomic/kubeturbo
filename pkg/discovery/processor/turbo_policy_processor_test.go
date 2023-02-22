package processor

import (
	"encoding/json"
	"testing"

	mapset "github.com/deckarep/golang-set"
	"github.com/stretchr/testify/assert"
	policyv1alpha1 "github.com/turbonomic/turbo-policy/api/v1alpha1"
	"github.com/turbonomic/turbo-go-sdk/pkg/proto"
	api "k8s.io/api/core/v1"
	apiextensionv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/turbonomic/kubeturbo/pkg/discovery/repository"
	"github.com/turbonomic/kubeturbo/pkg/discovery/worker"
)

var (
	minReplicas           = int32(1)
	maxReplicas           = int32(10)
	responseTime          = 300.1
	transaction           = 20
	marshaledRT, _        = json.Marshal(responseTime)
	marshaledTRX, _       = json.Marshal(transaction)
	marshaledRTInvalid, _ = json.Marshal(-1)
	scaleUp               = policyv1alpha1.Automatic
	scaleDown             = policyv1alpha1.Disabled
	sloScaleTypeMeta      = v1.TypeMeta{
		Kind:       "SLOHorizontalScale",
		APIVersion: "policy.turbonomic.io/v1alpha1",
	}
	sloScaleObjMeta = v1.ObjectMeta{
		Name:      "slo-horizontal-scale-sample",
		Namespace: "turbonomic",
		UID:       "b05990b9-e0f8-43b8-8f09-f4223c6711c9",
	}
	sloScaleSpec = policyv1alpha1.SLOHorizontalScaleSpec{
		MinReplicas: &minReplicas,
		MaxReplicas: &maxReplicas,
		Objectives: []policyv1alpha1.SLOHorizontalScalePolicySetting{
			{
				Name:  "ResponseTime",
				Value: apiextensionv1.JSON{Raw: marshaledRT},
			},
			{
				Name:  "Transaction",
				Value: apiextensionv1.JSON{Raw: marshaledTRX},
			},
		},
		Behavior: policyv1alpha1.ActionBehavior{
			HorizontalScaleUp:   &scaleUp,
			HorizontalScaleDown: &scaleDown,
		},
	}
	sloScaleSpecMinReplicasLargerThanMaxReplicas = policyv1alpha1.SLOHorizontalScaleSpec{
		MinReplicas: &maxReplicas,
		MaxReplicas: &minReplicas,
		Objectives: []policyv1alpha1.SLOHorizontalScalePolicySetting{
			{
				Name:  "ResponseTime",
				Value: apiextensionv1.JSON{Raw: marshaledRT},
			},
		},
	}
	sloScaleSpecInvalidObjectives = policyv1alpha1.SLOHorizontalScaleSpec{
		Objectives: []policyv1alpha1.SLOHorizontalScalePolicySetting{
			{
				Name:  "ResponseTime",
				Value: apiextensionv1.JSON{Raw: marshaledRTInvalid},
			},
		},
	}
	pbTypeMeta = v1.TypeMeta{
		Kind:       "PolicyBinding",
		APIVersion: "policy.turbonomic.io/v1alpha1",
	}
	pbObjMeta = v1.ObjectMeta{
		Name:      "policy-binding-sample",
		Namespace: "turbonomic",
		UID:       "ca723a93-f224-43e9-9a4e-c80ebecc09f5",
	}
	pbObjMeta1 = v1.ObjectMeta{
		Name:      "policy-binding-sample1",
		Namespace: "turbonomic",
		UID:       "ca723a93-f224-43e9-9a4e-c80ebecc09f6",
	}
	pbTargets = []policyv1alpha1.PolicyTargetReference{
		{
			Kind: "Service",
			Name: ".*(topology-processor|group|repository|api)",
		},
		{
			Kind: "Service",
			Name: ".*mediation.*",
		},
		{
			Kind: "Service",
			Name: ".*history",
		},
	}
	pbSpec = policyv1alpha1.PolicyBindingSpec{
		PolicyRef: policyv1alpha1.PolicyReference{
			Kind: sloScaleTypeMeta.Kind,
			Name: sloScaleObjMeta.Name,
		},
		Targets: pbTargets,
	}
	pbSpecNoTarget = policyv1alpha1.PolicyBindingSpec{
		PolicyRef: policyv1alpha1.PolicyReference{
			Kind: sloScaleTypeMeta.Kind,
			Name: sloScaleObjMeta.Name,
		},
	}
	svc1 = &api.Service{
		TypeMeta: v1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      "topology-processor",
			Namespace: "turbonomic",
			UID:       "topology-processor",
		},
	}
	svc2 = &api.Service{
		TypeMeta: v1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      "api",
			Namespace: "default",
			UID:       "api",
		},
	}
	svc3 = &api.Service{
		TypeMeta: v1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      "history",
			Namespace: "turbonomic",
			UID:       "history",
		},
	}
	kubeCluster = repository.NewKubeCluster(testClusterName,
		createMockNodes(allocatableMap, schedulableNodeMap))
)

func createValidSLOHorizontalScale() policyv1alpha1.SLOHorizontalScale {
	return policyv1alpha1.SLOHorizontalScale{
		TypeMeta:   sloScaleTypeMeta,
		ObjectMeta: sloScaleObjMeta,
		Spec:       sloScaleSpec,
	}
}

func createSLOHorizontalScaleWithInvalidReplicas() policyv1alpha1.SLOHorizontalScale {
	return policyv1alpha1.SLOHorizontalScale{
		TypeMeta:   sloScaleTypeMeta,
		ObjectMeta: sloScaleObjMeta,
		Spec:       sloScaleSpecMinReplicasLargerThanMaxReplicas,
	}
}

func createSLOHorizontalScaleWithInvalidObjectives() policyv1alpha1.SLOHorizontalScale {
	return policyv1alpha1.SLOHorizontalScale{
		TypeMeta:   sloScaleTypeMeta,
		ObjectMeta: sloScaleObjMeta,
		Spec:       sloScaleSpecInvalidObjectives,
	}
}

func createValidTurboPolicyBinding() policyv1alpha1.PolicyBinding {
	return policyv1alpha1.PolicyBinding{
		TypeMeta:   pbTypeMeta,
		ObjectMeta: pbObjMeta,
		Spec:       pbSpec,
	}
}

func createValidTurboPolicyBinding1() policyv1alpha1.PolicyBinding {
	return policyv1alpha1.PolicyBinding{
		TypeMeta:   pbTypeMeta,
		ObjectMeta: pbObjMeta1,
		Spec:       pbSpec,
	}
}

func createTurboPolicyBindingsWithNoTarget() policyv1alpha1.PolicyBinding {
	return policyv1alpha1.PolicyBinding{
		TypeMeta:   pbTypeMeta,
		ObjectMeta: pbObjMeta,
		Spec:       pbSpecNoTarget,
	}
}

func TestProcessTwoPolicyBindingsWithTheSamePolicy(t *testing.T) {
	clusterScrapper := &MockClusterScrapper{
		mockGetAllTurboSLOScalings: func() ([]policyv1alpha1.SLOHorizontalScale, error) {
			return []policyv1alpha1.SLOHorizontalScale{
				createValidSLOHorizontalScale(),
			}, nil
		},
		mockGetAllTurboPolicyBindings: func() ([]policyv1alpha1.PolicyBinding, error) {
			return []policyv1alpha1.PolicyBinding{
				createValidTurboPolicyBinding(),
				createValidTurboPolicyBinding1(),
			}, nil
		},
	}
	turboPolicyProcessor := &TurboPolicyProcessor{
		ClusterScraper: clusterScrapper,
		KubeCluster:    kubeCluster,
	}
	turboPolicyProcessor.ProcessTurboPolicies()
	policyBindings := kubeCluster.TurboPolicyBindings
	assert.Equal(t, 2, len(policyBindings))
	pbNames := mapset.NewSet()
	for _, pb := range policyBindings {
		pbNames.Add(pb.GetName())
	}
	assert.Equal(t, 2, pbNames.Cardinality())
}

func TestProcessValidTurboPolicy(t *testing.T) {
	clusterScrapper := &MockClusterScrapper{
		mockGetAllTurboSLOScalings: func() ([]policyv1alpha1.SLOHorizontalScale, error) {
			return []policyv1alpha1.SLOHorizontalScale{
				createValidSLOHorizontalScale(),
			}, nil
		},
		mockGetAllTurboPolicyBindings: func() ([]policyv1alpha1.PolicyBinding, error) {
			return []policyv1alpha1.PolicyBinding{
				createValidTurboPolicyBinding(),
			}, nil
		},
	}
	turboPolicyProcessor := &TurboPolicyProcessor{
		ClusterScraper: clusterScrapper,
		KubeCluster:    kubeCluster,
	}
	turboPolicyProcessor.ProcessTurboPolicies()
	policyBindings := kubeCluster.TurboPolicyBindings
	assert.Equal(t, 1, len(policyBindings))
	for _, policyBinding := range policyBindings {
		assert.Equal(t, sloScaleTypeMeta.Kind, policyBinding.GetPolicyType())
		assert.NotNil(t, policyBinding.GetSLOHorizontalScaleSpec())
		assert.Equal(t, 3, len(policyBinding.GetTargets()))
	}
	kubeCluster.TurboPolicyBindings = nil
}

func TestProcessTurboPolicyWithNoneMatchingPolicyRef(t *testing.T) {
	clusterScrapper := &MockClusterScrapper{
		mockGetAllTurboSLOScalings: func() ([]policyv1alpha1.SLOHorizontalScale, error) {
			return []policyv1alpha1.SLOHorizontalScale{}, nil
		},
		mockGetAllTurboPolicyBindings: func() ([]policyv1alpha1.PolicyBinding, error) {
			return []policyv1alpha1.PolicyBinding{
				createValidTurboPolicyBinding(),
			}, nil
		},
	}
	turboPolicyProcessor := &TurboPolicyProcessor{
		ClusterScraper: clusterScrapper,
		KubeCluster:    kubeCluster,
	}
	turboPolicyProcessor.ProcessTurboPolicies()
	policyBindings := kubeCluster.TurboPolicyBindings
	assert.Empty(t, policyBindings)
	kubeCluster.TurboPolicyBindings = nil
}

func TestProcessTurboPolicyWithEmptyTargetRef(t *testing.T) {
	clusterScrapper := &MockClusterScrapper{
		mockGetAllTurboSLOScalings: func() ([]policyv1alpha1.SLOHorizontalScale, error) {
			return []policyv1alpha1.SLOHorizontalScale{
				createValidSLOHorizontalScale(),
			}, nil
		},
		mockGetAllTurboPolicyBindings: func() ([]policyv1alpha1.PolicyBinding, error) {
			return []policyv1alpha1.PolicyBinding{
				createTurboPolicyBindingsWithNoTarget(),
			}, nil
		},
	}
	turboPolicyProcessor := &TurboPolicyProcessor{
		ClusterScraper: clusterScrapper,
		KubeCluster:    kubeCluster,
	}
	turboPolicyProcessor.ProcessTurboPolicies()
	policyBindings := kubeCluster.TurboPolicyBindings
	assert.Empty(t, policyBindings)
	kubeCluster.TurboPolicyBindings = nil
}

func TestConvertTurboPolicy(t *testing.T) {
	sloScale := createValidSLOHorizontalScale()
	turboPolicyBinding := createValidTurboPolicyBinding()
	turboPolicy := repository.NewTurboPolicy().WithSLOHorizontalScale(&sloScale)
	policyBinding := repository.
		NewTurboPolicyBinding(&turboPolicyBinding).
		WithTurboPolicy(turboPolicy)
	kubeCluster.TurboPolicyBindings = append(kubeCluster.TurboPolicyBindings, policyBinding)
	kubeClusterSummary := repository.CreateClusterSummary(kubeCluster)
	groupDiscoveryWorker := worker.Newk8sEntityGroupDiscoveryWorker(kubeClusterSummary, "targetId")
	groupDTOs := groupDiscoveryWorker.BuildTurboPolicyDTOsFromPolicyBindings()
	// No services cached in group discovery worker, cannot resolve services
	assert.Empty(t, groupDTOs)
	// Add the services map and try again
	kubeClusterSummary.Services = map[*api.Service][]string{
		svc1: {},
		svc2: {},
		svc3: {},
	}
	groupDTOs = groupDiscoveryWorker.BuildTurboPolicyDTOsFromPolicyBindings()
	assert.Equal(t, 1, len(groupDTOs))
	validSettingType := []proto.GroupDTO_Setting_SettingType{
		proto.GroupDTO_Setting_RESPONSE_TIME_SLO,
		proto.GroupDTO_Setting_TRANSACTION_SLO,
		proto.GroupDTO_Setting_MIN_REPLICAS,
		proto.GroupDTO_Setting_MAX_REPLICAS,
		proto.GroupDTO_Setting_HORIZONTAL_SCALE_UP_AUTOMATION_MODE,
		proto.GroupDTO_Setting_HORIZONTAL_SCALE_DOWN_AUTOMATION_MODE,
	}
	for _, groupDTO := range groupDTOs {
		// Verify membership
		members := groupDTO.GetMemberList().GetMember()
		assert.Equal(t, 2, len(members))
		assert.Contains(t, members, "topology-processor")
		assert.Contains(t, members, "history")
		settings := groupDTO.GetSettingPolicy().GetSettings()
		assert.Equal(t, 6, len(settings))
		for _, setting := range settings {
			assert.Contains(t, validSettingType, setting.GetType())
			if setting.GetType() == proto.GroupDTO_Setting_RESPONSE_TIME_SLO {
				assert.Equal(t, float32(responseTime), setting.GetNumericSettingValueType().GetValue())
			}
			if setting.GetType() == proto.GroupDTO_Setting_TRANSACTION_SLO {
				assert.Equal(t, float32(transaction), setting.GetNumericSettingValueType().GetValue())
			}
		}
	}
	kubeCluster.TurboPolicyBindings = nil
}

func TestConvertTurboPolicyWithInvalidReplicas(t *testing.T) {
	sloScale := createSLOHorizontalScaleWithInvalidReplicas()
	turboPolicyBinding := createValidTurboPolicyBinding()
	turboPolicy := repository.NewTurboPolicy().WithSLOHorizontalScale(&sloScale)
	policyBinding := repository.
		NewTurboPolicyBinding(&turboPolicyBinding).
		WithTurboPolicy(turboPolicy)
	kubeCluster.TurboPolicyBindings = append(kubeCluster.TurboPolicyBindings, policyBinding)
	kubeClusterSummary := repository.CreateClusterSummary(kubeCluster)
	groupDiscoveryWorker := worker.Newk8sEntityGroupDiscoveryWorker(kubeClusterSummary, "targetId")
	kubeClusterSummary.Services = map[*api.Service][]string{
		svc1: {},
	}
	groupDTOs := groupDiscoveryWorker.BuildTurboPolicyDTOsFromPolicyBindings()
	assert.Equal(t, 0, len(groupDTOs))
	kubeCluster.TurboPolicyBindings = nil
}

func TestConvertTurboPolicyWithInvalidObjectives(t *testing.T) {
	sloScale := createSLOHorizontalScaleWithInvalidObjectives()
	turboPolicyBinding := createValidTurboPolicyBinding()
	turboPolicy := repository.NewTurboPolicy().WithSLOHorizontalScale(&sloScale)
	policyBinding := repository.
		NewTurboPolicyBinding(&turboPolicyBinding).
		WithTurboPolicy(turboPolicy)
	kubeCluster.TurboPolicyBindings = append(kubeCluster.TurboPolicyBindings, policyBinding)
	kubeClusterSummary := repository.CreateClusterSummary(kubeCluster)
	groupDiscoveryWorker := worker.Newk8sEntityGroupDiscoveryWorker(kubeClusterSummary, "targetId")
	kubeClusterSummary.Services = map[*api.Service][]string{
		svc1: {},
	}
	groupDTOs := groupDiscoveryWorker.BuildTurboPolicyDTOsFromPolicyBindings()
	assert.Equal(t, 0, len(groupDTOs))
	kubeCluster.TurboPolicyBindings = nil
}
