package stitching

import (
	"strings"
	"testing"

	api "k8s.io/api/core/v1"
)

func mockAWSVolume(volID string) *api.PersistentVolume {
	vol := &api.PersistentVolume{}
	vol.Spec.AWSElasticBlockStore = &api.AWSElasticBlockStoreVolumeSource{
		VolumeID: volID,
	}
	return vol
}

func mockAzureVolume(volID string) *api.PersistentVolume {
	vol := &api.PersistentVolume{}
	vol.Spec.AzureDisk = &api.AzureDiskVolumeSource{
		DataDiskURI: volID,
	}
	return vol
}

func mockCSIAWSVolume(volID string) *api.PersistentVolume {
	vol := &api.PersistentVolume{}
	vol.Spec.CSI = &api.CSIPersistentVolumeSource{
		Driver:       "disk.csi.aws.com",
		VolumeHandle: volID,
	}
	return vol
}

func mockCSIAzureVolume(volID string) *api.PersistentVolume {
	vol := &api.PersistentVolume{}
	vol.Spec.CSI = &api.CSIPersistentVolumeSource{
		Driver:       "disk.csi.azure.com",
		VolumeHandle: volID,
	}
	return vol
}

func TestAWSVolumeUUIDGetter_GetUUID(t *testing.T) {
	type args struct {
		volumeID string
		labels   map[string]string
	}
	tests := []struct {
		args    args
		want    string
		wantErr bool
	}{
		{
			args{"aws://us-east-2c/vol-0e4eaa3ef79bcb5a9", map[string]string{}},
			"aws::us-east-2::VL::vol-0e4eaa3ef79bcb5a9",
			false,
		},
		{
			args{"vol-0e4eaa3ef79bcb5a9", map[string]string{"topology.kubernetes.io/zone": "us-east-2c"}},
			"aws::us-east-2::VL::vol-0e4eaa3ef79bcb5a9",
			false,
		},
		{
			args{"vol-0e4eaa3ef79bcb5a9", map[string]string{"topology.kubernetes.io/region": "us-east-2"}},
			"aws::us-east-2::VL::vol-0e4eaa3ef79bcb5a9",
			false,
		},

		{
			args{"vol-0e4eaa3ef79bcb5a9", map[string]string{}},
			"",
			true,
		},
	}

	getter := &awsVolumeUUIDGetter{}

	for _, test := range tests {
		vol := mockAWSVolume(test.args.volumeID)
		vol.Labels = test.args.labels
		result, err := getter.GetVolumeUUID(vol)

		if (err != nil) != test.wantErr {
			t.Errorf("Failed to get AWS node UUID: %v", err)
			continue
		}
		if strings.Compare(result, test.want) != 0 {
			t.Errorf("Wrong volume stitching UUID %v Vs. %v", result, test.want)
		}
	}
}

func TestAzureVolumeUUIDGetter_GetUUID(t *testing.T) {
	tests := [][]string{
		{
			"/subscriptions/6a5d73a4-e446-4c75-8f18-073b2f60d851/resourceGroups/mc_adveng_aks-virtual_westus/providers/Microsoft.Compute/disks/kubernetes-dynamic-pvc-0a2016c8-095c-481e-800d-684e277234e4",
			"::subscriptions::6a5d73a4-e446-4c75-8f18-073b2f60d851::resourcegroups::mc_adveng_aks-virtual_westus::providers::microsoft.compute::disks::kubernetes-dynamic-pvc-0a2016c8-095c-481e-800d-684e277234e4",
		},
	}

	getter := &azureVolumeUUIDGetter{}

	for _, pair := range tests {
		vol := mockAzureVolume(pair[0])
		result, err := getter.GetVolumeUUID(vol)

		if err != nil {
			t.Errorf("Failed to get Azure node UUID: %v", err)
			continue
		}

		if strings.Compare(result, pair[1]) != 0 {
			t.Errorf("Wrong volume stitching UUID %v Vs. %v", result, pair[1])
		}
	}
}

func TestCSIVolumeUUIDGetter_GetUUID(t *testing.T) {
	tests := [][]string{
		{
			"aws",
			"aws://us-east-2c/vol-0e4eaa3ef79bcb5a9",
			"aws::us-east-2::VL::vol-0e4eaa3ef79bcb5a9",
		},
		{
			"azure",
			"/subscriptions/6a5d73a4-e446-4c75-8f18-073b2f60d851/resourceGroups/mc_adveng_aks-virtual_westus/providers/Microsoft.Compute/disks/kubernetes-dynamic-pvc-0a2016c8-095c-481e-800d-684e277234e4",
			"::subscriptions::6a5d73a4-e446-4c75-8f18-073b2f60d851::resourcegroups::mc_adveng_aks-virtual_westus::providers::microsoft.compute::disks::kubernetes-dynamic-pvc-0a2016c8-095c-481e-800d-684e277234e4",
		},
	}

	getter := &csiVolumeUUIDGetter{}

	for _, pair := range tests {
		var vol *api.PersistentVolume
		if pair[0] == "aws" {
			vol = mockCSIAWSVolume(pair[1])
		} else if pair[0] == "azure" {
			vol = mockCSIAzureVolume(pair[1])
		}
		result, err := getter.GetVolumeUUID(vol)

		if err != nil {
			t.Errorf("Failed to get csi based %s node UUID: %v", pair[0], err)
			continue
		}

		if strings.Compare(result, pair[2]) != 0 {
			t.Errorf("Wrong volume stitching UUID %v Vs. %v", result, pair[2])
		}
	}
}
