package stitching

import (
	"bytes"
	"fmt"
	"github.com/golang/glog"
	api "k8s.io/client-go/pkg/api/v1"
	"strings"
)

const (
	awsPrefix     = "aws:///"
	azurePrefix   = "azure:///"
	uuidSeparator = "-"

	awsFormat   = "aws::%v::VM::%v"
	azureFormat = "azure::VM::%v"
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
	uuid := node.Status.NodeInfo.SystemUUID
	if len(uuid) < 1 {
		glog.Errorf("Node uuid is empty: %++v", node)
		return "", fmt.Errorf("Empty uuuid")
	}
	uuid = strings.ToLower(uuid)
	return uuid, nil
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

  Output:  azure::VM::e43fddd4-317a-4fc7-bba7-3ae729eaba6e

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

	//1. get partially reversed uuid
	suuid = strings.ToLower(suuid)
	uuid, err := reverseUuid(suuid)
	if err != nil {
		glog.Errorf("Failed to reverse Azure uuid: %v", err)
		return "", err
	}

	result := fmt.Sprintf(azureFormat, uuid)

	//2. TODO: generate a HASH of result, if len(result) > 80
	return result, nil
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
