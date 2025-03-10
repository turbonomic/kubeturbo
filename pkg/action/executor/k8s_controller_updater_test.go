package executor

import (
	"reflect"
	"testing"

	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGetSupportedResUsingKind(t *testing.T) {
	testCases := []struct {
		testName  string
		kind      string
		namespace string
		name      string
		res       schema.GroupVersionResource
		wantErr   bool
	}{
		{
			testName:  "test ReplicationController kind",
			kind:      util.KindReplicationController,
			namespace: "namesapce",
			name:      "name",
			res: schema.GroupVersionResource{
				Group:    util.K8sAPIReplicationControllerGV.Group,
				Version:  util.K8sAPIReplicationControllerGV.Version,
				Resource: util.ReplicationControllerResName},
			wantErr: false,
		},
		{
			testName:  "test ReplicaSet kind",
			kind:      util.KindReplicaSet,
			namespace: "namesapce",
			name:      "name",
			res: schema.GroupVersionResource{
				Group:    util.K8sAPIReplicasetGV.Group,
				Version:  util.K8sAPIReplicasetGV.Version,
				Resource: util.ReplicaSetResName},
			wantErr: false,
		},
		{
			testName:  "test Deployment kind",
			kind:      util.KindDeployment,
			namespace: "namesapce",
			name:      "name",
			res: schema.GroupVersionResource{
				Group:    util.K8sAPIDeploymentGV.Group,
				Version:  util.K8sAPIDeploymentGV.Version,
				Resource: util.DeploymentResName},
			wantErr: false,
		},
		{
			testName:  "test DeploymentConfig kind",
			kind:      util.KindDeploymentConfig,
			namespace: "namesapce",
			name:      "name",
			res: schema.GroupVersionResource{
				Group:    util.OpenShiftAPIDeploymentConfigGV.Group,
				Version:  util.OpenShiftAPIDeploymentConfigGV.Version,
				Resource: util.DeploymentConfigResName},
			wantErr: false,
		},
		{
			testName:  "test DaemonSet kind",
			kind:      util.KindDaemonSet,
			namespace: "namesapce",
			name:      "name",
			res: schema.GroupVersionResource{
				Group:    util.K8sAPIDaemonsetGV.Group,
				Version:  util.K8sAPIDaemonsetGV.Version,
				Resource: util.DaemonSetResName},
			wantErr: false,
		},
		{
			testName:  "test StatefulSet kind",
			kind:      util.KindStatefulSet,
			namespace: "namesapce",
			name:      "name",
			res: schema.GroupVersionResource{
				Group:    util.K8sAPIStatefulsetGV.Group,
				Version:  util.K8sAPIStatefulsetGV.Version,
				Resource: util.StatefulSetResName},
			wantErr: false,
		},
		{
			testName:  "test Job kind",
			kind:      util.KindJob,
			namespace: "namesapce",
			name:      "name",
			res:       schema.GroupVersionResource{},
			wantErr:   true,
		},
		{
			testName:  "test Virtual Machine kind",
			kind:      util.KindVirtualMachine,
			namespace: "namesapce",
			name:      "name",
			res:       util.OpenShiftVirtualMachineGVR,
			wantErr:   false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.testName, func(t *testing.T) {
			res, err := GetSupportedResUsingKind(testCase.kind, testCase.namespace, testCase.name)
			if (err != nil) != testCase.wantErr {
				t.Errorf("GetSupportedResUsingKind() error = %v, wantError %v", err, testCase.wantErr)
				return
			}
			if !reflect.DeepEqual(res, testCase.res) {
				t.Errorf("GetSupportedResUsingKind() got = %v, want %v", res, testCase.res)
			}
		})
	}
}
