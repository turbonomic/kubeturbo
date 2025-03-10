package resourcemapping

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"text/template"

	devopsv1alpha1 "github.ibm.com/turbonomic/orm/api/v1alpha1"

	"github.com/golang/glog"
	discoveryutil "github.ibm.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.ibm.com/turbonomic/kubeturbo/pkg/util"
	v1 "k8s.io/api/core/v1"
	apix "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

const (
	ormGroup    = "turbonomic.com"
	ormVersion  = "v1alpha1"
	ormResource = "operatorresourcemappings"

	resourceMappingComponentName = "componentName"
	resourceMappingSrcPath       = "srcPath"
	resourceMappingDestPath      = "destPath"
)

var (
	ormResourceMappingsPath           = []string{"spec", "resourceMappings"}
	srcResourceSpecKindPath           = []string{"srcResourceSpec", "kind"}
	srcResourceSpecComponentNamesPath = []string{"srcResourceSpec", "componentNames"}
	resourceMappingTemplatesPath      = []string{"resourceMappingTemplates"}
)

type ORMTemplate struct {
	// Name of a K8s resource (controller) to which we srcPath to update data
	componentName string
	// Slice of resourceMappingTemplates defined in ORM CR
	resourceMappingTemplates []map[string]interface{} //key -> source?
}

// ORMSpec defines the spec data which are used to update a corresponding Operator managed CR in action execution client.
type ORMSpec struct {
	operatorGVResource schema.GroupVersionResource
	// Map from componentKey ("controllerKind/componentName") to ORMTemplate
	ormTemplateMap map[string]ORMTemplate // source/owned kind/name -> path mappings
	// Is cluster scope or namespace scope
	isClusterScope bool
}

// ORMClient builds operator resource mapping templates fetched from OperatorResourceMapping CR in discovery client
// and provides the capability to update the corresponding Operator managed CR in action execution client.
type ORMClient struct {
	cacheLock    sync.Mutex
	dynClient    dynamic.Interface
	apiExtClient *apiextclient.ApiextensionsV1Client
	// Cached map data from Operator-managed CustomResource UID to ORMSpec. The cached data is updated each discovery.
	operatorResourceSpecMap map[string]*ORMSpec //key is owner UID
}

func NewORMClient(dynamicClient dynamic.Interface, apiExtClient *apiextclient.ApiextensionsV1Client) *ORMClient {
	return &ORMClient{
		dynClient:               dynamicClient,
		apiExtClient:            apiExtClient,
		operatorResourceSpecMap: make(map[string]*ORMSpec),
	}
}

// CacheORMSpecMap clears cached operatorResourceSpecMap data and repopulate the map based on newly discovered Operator
// managed CRs.
// The map is from Operator managed CustomResource UID to ORMSpec object. Here's an example of the map:
// {
//
//	  "b4ce6060-93be-11ea-9406-005056b83d00": {
//	   operatorGVResource  schema.GroupVersionResource
//
//     "Deployment/group": {
//       "componentName": "group",
//       "resourceMappingTemplates":
//         [
//          {
//            "srcPath": ".spec.template.spec.containers[?(@.name=="{{.componentName}}")].resources"
//            "destPath": ".spec.{{.componentName}}.resources"
//          },
//         ]
//     }
//   }
// }
func (ormClient *ORMClient) CacheORMSpecMap() int {
	ormCRs, err := ormClient.getORMCRList()
	if err != nil {
		glog.Warningf("No OperatorResourceMapping V1 CR discovered: %v. Create operator-resource-mapping CR to control "+
			"Operator managed resources.", err)
		return 0
	}
	ormClient.cacheLock.Lock()
	defer ormClient.cacheLock.Unlock()
	// Clear existing cached operatorResourceSpecMap data
	ormClient.operatorResourceSpecMap = make(map[string]*ORMSpec)
	for _, ormCR := range ormCRs {
		// An orm CR name always refers to a Operator manged CRD name
		crdName := ormCR.GetName()
		namespace := ormCR.GetNamespace()
		// Get Operator CRs of the CRD in the same namespace of ORM CR.
		operatorCRs, gvRes, isClusterScope, err := ormClient.getOperatorCRsFromCRD(crdName, namespace)
		if err != nil {
			glog.Errorf("Failed to get Operator managed CRs from CRD %s, %v", crdName, err)
			continue
		}
		ormTemplateMap, err := ormClient.populateORMTemplateMap(ormCR)
		if err != nil {
			glog.Errorf("Failed to populate ormTemplateMap for CRD %s: %v", crdName, err)
			continue
		}
		for _, operatorCR := range operatorCRs {
			crUID := string(operatorCR.GetUID())
			operatorResourceSpec, exists := ormClient.operatorResourceSpecMap[crUID]
			if exists {
				glog.Errorf("Skip the duplicate CR UID '%s' for CRD %s", crUID, crdName)
				continue
			} else {
				operatorResourceSpec = &ORMSpec{
					operatorGVResource: gvRes,
					ormTemplateMap:     ormTemplateMap,
					isClusterScope:     isClusterScope,
				}
				ormClient.operatorResourceSpecMap[crUID] = operatorResourceSpec
			}
		}
	}
	return len(ormClient.operatorResourceSpecMap)
}

func (ormClient *ORMClient) getORMCRList() ([]unstructured.Unstructured, error) {
	groupVersionRes := schema.GroupVersionResource{
		Group:    ormGroup,
		Version:  ormVersion,
		Resource: ormResource,
	}
	ormCRs, err := ormClient.dynClient.Resource(groupVersionRes).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get OperatorResourceMapping CRs: %v", err)
	}
	return ormCRs.Items, nil
}

// this will help determine to pick right crd version for XL that's in use by checking if storage=true
func GetCRDWithStorageVersionName(crd *apix.CustomResourceDefinition) (string, error) {
	for _, version := range crd.Spec.Versions {
		if version.Storage {
			return version.Name, nil
		}
	}
	// This should not happen if crd is valid
	return "", fmt.Errorf("invalid CustomResourceDefinition, no storage version")
}

func (ormClient *ORMClient) getOperatorCRsFromCRD(crdName, namespace string) ([]unstructured.Unstructured, schema.GroupVersionResource, bool, error) {
	// First get the Operator CRD
	crds := ormClient.apiExtClient.CustomResourceDefinitions() //RESTClient is used to communicate
	// with API server by this client implementation.
	crd, err := crds.Get(context.TODO(), crdName, metav1.GetOptions{})
	var isClusterScope bool
	if err != nil {
		return nil, schema.GroupVersionResource{}, isClusterScope, err
	}
	crdVersionName, err := GetCRDWithStorageVersionName(crd)
	if err != nil {
		return nil, schema.GroupVersionResource{}, isClusterScope, err
	}
	groupVersionRes := schema.GroupVersionResource{
		Group:    crd.Spec.Group,
		Version:  crdVersionName,
		Resource: crd.Spec.Names.Plural,
	}

	// Next get the CRs
	var crs *unstructured.UnstructuredList
	if crd.Spec.Scope == apix.NamespaceScoped {
		crs, err = ormClient.dynClient.Resource(groupVersionRes).Namespace(namespace).List(context.TODO(), metav1.ListOptions{})
	} else {
		crs, err = ormClient.dynClient.Resource(groupVersionRes).Namespace(v1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
		isClusterScope = true
	}

	if err != nil {
		return nil, groupVersionRes, isClusterScope, err
	}
	return crs.Items, groupVersionRes, isClusterScope, nil
}

func (ormClient *ORMClient) populateORMTemplateMap(ormCR unstructured.Unstructured) (map[string]ORMTemplate, error) {
	ormCRName := ormCR.GetName()
	ormTemplateMap := make(map[string]ORMTemplate)
	resourceMappings, _, err := unstructured.NestedSlice(ormCR.Object, ormResourceMappingsPath...)
	if err != nil {
		return nil, fmt.Errorf("no data under '%s' in ORM CR %s: %v", util.JSONPath(ormResourceMappingsPath), ormCRName, err)
	}
	for rmInd, resourceMapping := range resourceMappings {
		rm, ok := resourceMapping.(map[string]interface{})
		if !ok {
			glog.Errorf("ResourceMappings[%v] does not have the expected 'map[string]interface{}' structure in ORM CR %s", rmInd, ormCRName)
			continue
		}
		srcResourceKind, found, err := unstructured.NestedString(rm, srcResourceSpecKindPath...)
		if err != nil || !found {
			glog.Errorf("Value is not found under '%s' under '%s' in ORM CR %s", util.JSONPath(srcResourceSpecKindPath),
				util.JSONPath(ormResourceMappingsPath), ormCRName)
			continue
		}
		componentNames, found, err := unstructured.NestedStringSlice(rm, srcResourceSpecComponentNamesPath...)
		if err != nil || !found {
			glog.Errorf("Value is not found under '%s' under '%s' in ORM CR %s", util.JSONPath(srcResourceSpecComponentNamesPath),
				util.JSONPath(ormResourceMappingsPath), ormCRName)
			continue
		}
		resourceMappingTemplates, found, err := unstructured.NestedSlice(rm, resourceMappingTemplatesPath...)
		if err != nil || !found {
			glog.Errorf("Value not found under '%s' under '%s' in ORM CR %s", util.JSONPath(resourceMappingTemplatesPath),
				util.JSONPath(ormResourceMappingsPath), ormCRName)
			continue
		}

		rmTemplates := make([]map[string]interface{}, 0, len(resourceMappingTemplates))
		for rmtInd, resourceMappingTemplate := range resourceMappingTemplates {
			rmTemplate, ok := resourceMappingTemplate.(map[string]interface{})
			if !ok {
				glog.Errorf("ResourceMappings[%v] resourceMappingTemplates[%v] does not have the expected 'map[string]interface{}' structure in ORM CR %s", rmInd, rmtInd, ormCRName)
				continue
			}
			rmTemplates = append(rmTemplates, rmTemplate)
		}
		for _, componentName := range componentNames {
			componentKey := srcResourceKind + "/" + componentName
			ormSpec := ORMTemplate{
				componentName:            componentName,
				resourceMappingTemplates: rmTemplates,
			}
			ormTemplateMap[componentKey] = ormSpec
		}
	}
	return ormTemplateMap, nil
}

func createObjRef(obj *unstructured.Unstructured) v1.ObjectReference {
	objRef := v1.ObjectReference{
		Kind:       obj.GetKind(),
		Namespace:  obj.GetNamespace(),
		Name:       obj.GetName(),
		APIVersion: obj.GetAPIVersion(),
	}
	return objRef
}

func defaultResourcePathWithOwned(owned []*devopsv1alpha1.ResourcePath, ownedObj *unstructured.Unstructured) map[string][]devopsv1alpha1.ResourcePath {
	ownedV1Resources := make(map[string][]devopsv1alpha1.ResourcePath)

	ownedRef := createObjRef(ownedObj)

	for _, ownedRespath := range owned {
		ownedPath := ownedRespath.Path
		ownedResPath := &devopsv1alpha1.ResourcePath{
			ObjectReference: ownedRef,
			Path:            ownedPath,
		}
		ownedV1Resources[ownedPath] = []devopsv1alpha1.ResourcePath{*ownedResPath}
	}

	return ownedV1Resources
}

func (ormClient *ORMClient) retrieveOwnerResource(ownedObj *unstructured.Unstructured, ownerReference discoveryutil.OwnerInfo) (*ORMSpec, *unstructured.Unstructured, error) {
	operatorCRUID := string(ownerReference.Uid)               // owner is considered as operator
	ownedKey := ownedObj.GetKind() + "/" + ownedObj.GetName() // source or owned resource

	operatorResourceSpec, exists := ormClient.operatorResourceSpecMap[operatorCRUID] //orm for the operator/owner
	if !exists {
		return nil, nil, fmt.Errorf("operatorResourceSpec not found in operatorResourceSpecMap for orm V1 operatorCR %s", operatorCRUID)
	}

	operatorResKind := ownerReference.Kind //operator kind and instance
	operatorResName := ownerReference.Name
	operatorRes := operatorResKind + "/" + operatorResName

	resourceNamespace := ownedObj.GetNamespace() //same namespace as the source/owned
	if operatorResourceSpec.isClusterScope {
		resourceNamespace = v1.NamespaceAll
	}
	dynResourceClient := ormClient.dynClient.Resource(operatorResourceSpec.operatorGVResource).Namespace(resourceNamespace)
	operatorCR, err := dynResourceClient.Get(context.TODO(), operatorResName, metav1.GetOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get orm V1 operatorCR %s for %s in namespace %s: %v",
			operatorRes, ownedKey, resourceNamespace, err)
	}

	return operatorResourceSpec, operatorCR, nil
}

func (ormClient *ORMClient) LocateOwnerPaths(ownedObj *unstructured.Unstructured, ownerReference discoveryutil.OwnerInfo, owned []*devopsv1alpha1.ResourcePath) (map[string][]devopsv1alpha1.ResourcePath, error) {
	ownedKey := ownedObj.GetKind() + "/" + ownedObj.GetName() // source or owned resource

	var allOwnerResourcePaths map[string][]devopsv1alpha1.ResourcePath
	owners := map[string][]devopsv1alpha1.ResourcePath{}

	orm, ownerObj, err := ormClient.retrieveOwnerResource(ownedObj, ownerReference)
	if err != nil {
		allOwnerResourcePaths = defaultResourcePathWithOwned(owned, ownedObj)
		return allOwnerResourcePaths, err
	}
	if orm == nil || ownerObj == nil {
		allOwnerResourcePaths = defaultResourcePathWithOwned(owned, ownedObj)
		return allOwnerResourcePaths, fmt.Errorf("cannot find orm V1 owner resource paths for sources : '%s' so returning owned/source resource paths", ownedKey)
	}

	ormTemplate, exists := orm.ormTemplateMap[ownedKey] //path mappings for the source
	if !exists {
		allOwnerResourcePaths = defaultResourcePathWithOwned(owned, ownedObj)
		return allOwnerResourcePaths, fmt.Errorf("ormTemplate not found in ormTemplateMap of orm V1 for componentKey %s for operatorCR %s",
			ownedKey, ownerObj.GetUID())
	}
	ownerRef := createObjRef(ownerObj)

	operatorRes := ownerObj.GetKind() + "/" + ownerObj.GetName()
	resourceNamespace := ownedObj.GetNamespace()
	// Update based on each resourceMappingTemplate
	for _, sourceResPath := range owned {
		sp := sourceResPath.Path
		for _, resourceMappingTemplate := range ormTemplate.resourceMappingTemplates {
			// Set resourceMappingComponentName to resourceMappingTemplate
			// so as to parse the srcPath and destPath based on text template
			resourceMappingTemplate[resourceMappingComponentName] = ormTemplate.componentName
			srcPath, destPath, err := ormClient.parseSrcAndDestPath(resourceMappingTemplate)
			if err != nil {
				return nil, fmt.Errorf("failed to update CR %s for %s in namespace %s: %v",
					operatorRes, ownedKey, resourceNamespace, err)
			}
			if srcPath == sp {
				owner := &devopsv1alpha1.ResourcePath{
					ObjectReference: ownerRef,
					Path:            destPath,
				}
				owners[sp] = []devopsv1alpha1.ResourcePath{*owner}
			}
		}
	}

	return owners, nil
}

func (ormClient *ORMClient) parseSrcAndDestPath(resourceMappingTemplate map[string]interface{}) (string, string, error) {
	parsedSrcPath, err := ormClient.parsePath(resourceMappingTemplate, resourceMappingSrcPath)
	if err != nil {
		return "", "", err
	}
	parsedDestPath, err := ormClient.parsePath(resourceMappingTemplate, resourceMappingDestPath)
	if err != nil {
		return "", "", err
	}
	return parsedSrcPath, parsedDestPath, nil
}

func (ormClient *ORMClient) parsePath(resourceMappingTemplate map[string]interface{}, pathType string) (string, error) {
	path, exists := resourceMappingTemplate[pathType] //interface type
	if !exists {
		return "", fmt.Errorf("%s does not exist in resourceMappingTemplate", pathType)
	}
	pathStr, ok := path.(string)
	if !ok {
		return "", fmt.Errorf("conversion error: %v is of the type %T, expected string", path, path)
	}
	srcTmpl, err := template.New(pathType).Parse(pathStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse %s %s: %v", pathType, pathStr, err)
	}
	buf := &bytes.Buffer{}
	err = srcTmpl.Execute(buf, resourceMappingTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse %s %s: %v", pathType, pathStr, err)
	}
	parsedPath := buf.String()
	return parsedPath, nil
}
