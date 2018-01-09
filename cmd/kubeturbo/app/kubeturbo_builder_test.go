package app

import (
	"testing"

	"github.com/turbonomic/kubeturbo/pkg/discovery/configs"
	"github.com/turbonomic/kubeturbo/pkg/discovery/monitoring/kubelet"
	"github.com/turbonomic/kubeturbo/pkg/discovery/stitching"
	restclient "k8s.io/client-go/rest"
)

func TestVMTServer_createProbeConfigOrDie(t *testing.T) {
	type args struct {
		kubeConfig    *restclient.Config
		kubeletClient *kubelet.KubeletClient
	}
	tests := []struct {
		name                      string
		CAdvisorPort              int
		UseVMWare                 bool
		args                      args
		wantCAdvisorPort          int
		wantStitchingPropertyType stitching.StitchingPropertyType
	}{
		{
			name:                      "test-use-vmware",
			CAdvisorPort:              1234,
			UseVMWare:                 false,
			args:                      args{kubeConfig: &restclient.Config{}, kubeletClient: &kubelet.KubeletClient{}},
			wantCAdvisorPort:          1234,
			wantStitchingPropertyType: stitching.IP,
		},
		{
			name:                      "test-not-use-vmware",
			CAdvisorPort:              5678,
			UseVMWare:                 true,
			args:                      args{kubeConfig: &restclient.Config{}, kubeletClient: &kubelet.KubeletClient{}},
			wantCAdvisorPort:          5678,
			wantStitchingPropertyType: stitching.UUID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &VMTServer{
				CAdvisorPort: tt.CAdvisorPort,
				UseVMWare:    tt.UseVMWare,
			}

			got := s.createProbeConfigOrDie(tt.args.kubeConfig, tt.args.kubeletClient)
			checkProbeConfig(t, got, tt.wantCAdvisorPort, tt.wantStitchingPropertyType)
		})
	}
}

func checkProbeConfig(t *testing.T, pc *configs.ProbeConfig, cadvisorPort int, stitchingPropertyType stitching.StitchingPropertyType) {
	if pc.CadvisorPort != cadvisorPort {
		t.Errorf("CadvisorPort = %v, want %v", pc.CadvisorPort, cadvisorPort)
	}

	if pc.StitchingPropertyType != stitchingPropertyType {
		t.Errorf("StitchingPropertyType = %v, want %v", pc.StitchingPropertyType, stitchingPropertyType)
	}
}
