package stitching

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/golang/glog"
	api "k8s.io/api/core/v1"
)

const (
	awsPrefix     = "aws:///"
	azurePrefix   = "azure:///"
	gcePrefix     = "gce://"
	vspherePrefix = "vsphere://"
	uuidSeparator = "-"

	awsFormat   = "aws::%v::VM::%v"
	azureFormat = "azure::VM::%v"
	gceFormat   = "gcp::%v::VM::%v"
)

type NodeUUIDGetter interface {
	GetUUID(node *api.Node) (string, error)
	Name() string
}

/**
  Input k8s.Node info
    spec:
      externalID: osp-node-1.eng.vmturbo.com
    nodeInfo:
      systemUUID: 4200979A-4EF9-E49B-6BD6-FDBAD2BE7252

  Output: 4200979a-4ef9-e49b-6bd6-fdbad2be7252
  (lower case of the systemUUID)
*/
type defaultNodeUUIDGetter struct {
}

func (d *defaultNodeUUIDGetter) Name() string {
	return "Default"
}

func (d *defaultNodeUUIDGetter) GetUUID(node *api.Node) (string, error) {
	suuid := node.Status.NodeInfo.SystemUUID
	if len(suuid) < 1 {
		glog.Errorf("Node uuid is empty: %++v", node)
		return "", fmt.Errorf("Empty uuid")
	}

	// the uuid is in lower case in vCenter Probe
	suuid = strings.ToLower(suuid)
	reversedSuuid, err := reverseUuid(suuid)
	if err != nil {
		glog.Warningf("Failed to reverse endianness of node %s's UUID %s: %v", node.Name, suuid, err)
		return suuid, nil
	} else {
		return fmt.Sprintf("%s,%s", suuid, reversedSuuid), nil
	}
}

/**
Input AWS.k8s.Node info:
 spec:
   externalID: i-0be85bb9db1707470
   podCIDR: 10.2.1.0/24
   providerID: aws:///us-west-2a/i-0be85bb9db1707470

 Output:  aws::us-west-2::VM::i-0be85bb9db1707470
*/

type awsNodeUUIDGetter struct {
}

func (aws *awsNodeUUIDGetter) Name() string {
	return "AWS"
}

func (aws *awsNodeUUIDGetter) GetUUID(node *api.Node) (string, error) {
	providerId := node.Spec.ProviderID
	if !strings.HasPrefix(providerId, awsPrefix) {
		glog.Errorf("Not a valid AWS node uuid: %++v", node)
		return "", fmt.Errorf("Invalid")
	}

	//2. split the suffix into two parts:
	// aws:///us-west-2a/i-0be85bb9db1707470 -> [us-west-2a, i-0be85bb9db1707470]
	suffix := providerId[len(awsPrefix):]
	parts := strings.Split(suffix, "/")
	if len(parts) != 2 {
		glog.Errorf("Failed to split uuid (%d): %v", len(parts), parts)
		return "", fmt.Errorf("Invalid")
	}

	//3. get region by remove the zone suffix
	if len(parts[0]) < 2 {
		glog.Errorf("Invalid zone Id: %v", providerId)
		return "", fmt.Errorf("Invalid")
	}

	end := len(parts[0]) - 1
	region := parts[0][0:end]

	result := fmt.Sprintf(awsFormat, region, parts[1])
	return result, nil
}

/**
  Input Azure.k8s.Node info:
  spec:
    externalID: /subscriptions/758ad253-cbf5-4b18-8863-3eed0825bf07/resourceGroups/spceastus2/providers/Microsoft.Compute/virtualMachines/spc1w695w0-master-1
    podCIDR: 10.2.0.0/24
    providerID: azure:///subscriptions/758ad253-cbf5-4b18-8863-3eed0825bf07/resourceGroups/spceastus2/providers/Microsoft.Compute/virtualMachines/spc1w695w0-master-1
  NodeInfo:
    systemUUID: D4DD3FE4-7A31-C74F-BBA7-3AE729EABA6E

  Output:  azure::VM::e43fddd4-317a-4fc7-bba7-3ae729eaba6e,azure::VM::D4DD3FE4-7A31-C74F-BBA7-3AE729EABA6E

  ref
  https://git.turbonomic.com/turbonomic/opsmgr/blob/develop/com.vmturbo.mediation.azure/src/main/java/com/vmturbo/mediation/azure/AzureUtility.java#L235
*/

type azureNodeUUIDGetter struct {
}

func (azure *azureNodeUUIDGetter) Name() string {
	return "Azure"
}

func (azure *azureNodeUUIDGetter) GetUUID(node *api.Node) (string, error) {
	providerId := node.Spec.ProviderID
	if !strings.HasPrefix(providerId, azurePrefix) {
		glog.Warningf("Not a Azure node: % ++v", node)
	}

	suuid := node.Status.NodeInfo.SystemUUID
	if len(suuid) < 1 {
		glog.Errorf("Node system uuid is empty: %++v", node)
		return "", fmt.Errorf("Empty uuid")
	}

	// we use both the reversed uuid and the actual systemUUID to cater to possibility
	// of both big and small endian environments.
	suuid = strings.ToLower(suuid)
	result := fmt.Sprintf(azureFormat, suuid)
	reversedSuuid, err := reverseUuid(suuid)
	if err != nil {
		glog.Warningf("Failed to reverse endianness of Azure node %s's UUID %s: %v", node.Name, suuid, err)
		return result, nil
	} else {
		reversedResult := fmt.Sprintf(azureFormat, reversedSuuid)
		return fmt.Sprintf("%s,%s", result, reversedResult), nil
	}
}

/**
Input GCE.k8s.Node info:
  metadata:
    annotations:
      container.googleapis.com/instance_id: "8108478110475488564"

  spec:
    providerID: gce://turbonomic-eng/us-central1-a/gke-enlin-cluster-1-default-pool-b0f2516c-mrl0

 Output:  gcp::us-central1-a::VM::8108478110475488564
*/

type gceNodeUUIDGetter struct {
}

func (gce *gceNodeUUIDGetter) Name() string {
	return "GCP"
}

func (gce *gceNodeUUIDGetter) GetUUID(node *api.Node) (string, error) {
	instanceId := node.ObjectMeta.Annotations["container.googleapis.com/instance_id"]
	providerId := node.Spec.ProviderID

	if len(instanceId) == 0 {
		glog.Errorf("Not a valid GCE instanceId: %++v", node)
		return "", fmt.Errorf("Invalid")
	}

	if !strings.HasPrefix(providerId, gcePrefix) {
		glog.Errorf("Not a valid GCE node uuid: %++v", node)
		return "", fmt.Errorf("Invalid")
	}

	//2. split the suffix into two parts:
	// gce://turbonomic-eng/us-central1-a/gke-enlin-cluster-1-default-pool-b0f2516c-mrl0 -> [us-central1-a, i-0be85bb9db1707470]
	suffix := providerId[len(gcePrefix):]
	parts := strings.Split(suffix, "/")
	if len(parts) != 3 {
		glog.Errorf("Failed to split uuid (%d): %v", len(parts), parts)
		return "", fmt.Errorf("Invalid")
	}

	result := fmt.Sprintf(gceFormat, parts[1], instanceId)
	return result, nil
}

/**
Input Vsphere.k8s.Node info:
 spec:
   // The systemUUID identifies a node uniquely in most older environments and
   // matches that discovered by vsphere.
   // In newer versions (eg. tanzu) the id discovered from insfrastructure is
   // populated in the providerID.
   // We observe both fields and if both are present we send both, else send only
   // the information deciphered from providerID. If we can, we also endian reverse
   // both ids and attach them too.
   systemUUID: 4200C244-1E27-8473-AB44-999195DE924C
   providerID: vsphere://29e465c7-74d4-4a63-9ce4-41a7c04ba01d

 Output:  29e465c7-74d4-4a63-9ce4-41a7c04ba01d,c765e429-d474-634a-9ce4-41a7c04ba01d,
		  4200c244-1e27-8473-ab44-999195de924c,44c20042-271e-7384-ab44-999195de924c
*/

type vsphereNodeUUIDGetter struct {
}

func (v *vsphereNodeUUIDGetter) Name() string {
	return "Vsphere"
}

func (v *vsphereNodeUUIDGetter) GetUUID(node *api.Node) (string, error) {
	providerId := node.Spec.ProviderID
	if !strings.HasPrefix(providerId, vspherePrefix) {
		glog.Errorf("Not a valid Vspehere node uuid: %++v", node)
		return "", fmt.Errorf("Invalid")
	}

	//1. Get the id from providerID string, include that and the reversed id
	//   29e465c7-74d4-4a63-9ce4-41a7c04ba01d,c765e429-d474-634a-9ce4-41a7c04ba01d
	providerID := strings.ToLower(providerId[len(vspherePrefix):])
	stitchingID := providerID
	reverseProviderID, err := reverseUuid(providerID)
	if err != nil {
		glog.Warningf("Failed to reverse endianness of Vsphere node %s's UUID %s: %v", node.Name, providerID, err)
	} else {
		stitchingID = fmt.Sprintf("%s,%s", providerID, reverseProviderID)
	}

	//2. Check if node.Status.NodeInfo.SystemUUID is filled by provider
	//   If it is, append that as another set of ids including the reversed id
	//   29e465c7-74d4-4a63-9ce4-41a7c04ba01d,c765e429-d474-634a-9ce4-41a7c04ba01d,
	//   4200c244-1e27-8473-ab44-999195de924c,44c20042-271e-7384-ab44-999195de924c
	suuid := strings.ToLower(node.Status.NodeInfo.SystemUUID)
	if suuid != "" && suuid != providerID {
		stitchingID = fmt.Sprintf("%s,%s", stitchingID, suuid)
		reversedSuuid, err := reverseUuid(suuid)
		if err != nil {
			glog.Warningf("Failed to reverse endianness of Vsphere node %s's UUID %s: %v", node.Name, suuid, err)
		} else {
			stitchingID = fmt.Sprintf("%s,%s", stitchingID, reversedSuuid)
		}
	}

	return stitchingID, nil
}

func reverseUuid(oid string) (string, error) {
	parts := strings.Split(oid, uuidSeparator)
	if len(parts) != 5 {
		return "", fmt.Errorf("Split failed")
	}

	var buf bytes.Buffer

	//1. reverse the first 3 segments
	for i := 0; i < 3; i++ {
		seg := parts[i]
		for end := len(seg); end > 0; end -= 2 {
			buf.WriteString(seg[end-2 : end])
		}
		buf.WriteString(uuidSeparator)
	}

	//2. append the last 2 segments
	buf.WriteString(parts[3])
	buf.WriteString(uuidSeparator)
	buf.WriteString(parts[4])

	return buf.String(), nil
}
