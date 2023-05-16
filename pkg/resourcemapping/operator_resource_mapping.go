package resourcemapping

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"text/template"

	devopsv1alpha1 "github.com/turbonomic/orm/api/v1alpha1"
	"github.com/turbonomic/orm/kubernetes"

	"github.com/golang/glog"
	discoveryutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/util"
	ormutils "github.com/turbonomic/orm/utils"
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
		glog.Warningf("No OperatorResourceMapping CR discovered: %v. Create operator-resource-mapping CR to control "+
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

func createV1OwnedResourcePath(owned []*devopsv1alpha1.ResourcePath, ownedObj *unstructured.Unstructured) map[string][]devopsv1alpha1.ResourcePath {
	ownedV1Resources := make(map[string][]devopsv1alpha1.ResourcePath)

	ownedRef := v1.ObjectReference{
		Kind:       ownedObj.GetKind(),
		Namespace:  ownedObj.GetNamespace(),
		Name:       ownedObj.GetName(),
		APIVersion: ownedObj.GetAPIVersion(),
	}

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
		allOwnerResourcePaths = createV1OwnedResourcePath(owned, ownedObj)
		return allOwnerResourcePaths, err
	}
	if orm == nil || ownerObj == nil {
		allOwnerResourcePaths = createV1OwnedResourcePath(owned, ownedObj)
		return allOwnerResourcePaths, fmt.Errorf("cannot find orm V1 owner resource paths for sources : '%s' so returning owned/source resource paths", ownedKey)
	}

	ormTemplate, exists := orm.ormTemplateMap[ownedKey] //path mappings for the source
	if !exists {
		allOwnerResourcePaths = createV1OwnedResourcePath(owned, ownedObj)
		return allOwnerResourcePaths, fmt.Errorf("ormTemplate not found in ormTemplateMap of orm V1 for componentKey %s for operatorCR %s",
			ownedKey, ownerObj.GetUID())
	}
	var ownerRef v1.ObjectReference = v1.ObjectReference{
		Kind:       ownerObj.GetKind(),
		Namespace:  ownerObj.GetNamespace(),
		Name:       ownerObj.GetName(),
		APIVersion: ownerObj.GetAPIVersion(),
	}

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

// UpdateOwners updates the corresponding owner CR for an owned manged resource
// updatedControllerObj -- updated K8s controller object based on Turbo actionItem, from which the resource value is fetched
//
//	and will be set to the corresponding CR
//
// controllerOwnerReference -- ownerReference of a K8s controller, which contains metadata of a owner CR
// ownerResources -- mapping of owner resource obj with owner resource path and source/owned controller obj
func (ormClient *ORMClientManager) UpdateOwners(updatedControllerObj *unstructured.Unstructured, controllerOwnerReference discoveryutil.OwnerInfo, ownerResources *OwnerResources) error {
	updated := false
	operatorResKind := controllerOwnerReference.Kind //operator kind and instance
	operatorResName := controllerOwnerReference.Name
	operatorRes := operatorResKind + "/" + operatorResName
	resourceNamespace := updatedControllerObj.GetNamespace()
	sourceResKind := updatedControllerObj.GetKind()
	sourceResName := updatedControllerObj.GetName()
	sourceRes := sourceResKind + "/" + sourceResName
	for ownedPath, resourcePaths := range ownerResources.OwnerResourcesMap {
		for _, resourcePath := range resourcePaths {
			// Retrieve the owner object and path
			ownerPath := resourcePath.Path
			ownerObj := resourcePath.ObjectReference
			ownerResKind := ownerObj.Kind
			ownerResName := ownerObj.Name
			ownerRes := ownerResKind + "/" + ownerResName
			ownerResNamespace := ownerObj.Namespace
			// ownerResources might have source/owned resource kind with their resource paths if it cannot find the owner resource mapping from ORM.
			// so we check if the owner kind and the contoller kind we get from action is same, in that case we cannot perform this update operation
			// on source/owned resource kind without owner resource found
			if ownerResKind == updatedControllerObj.GetKind() {
				glog.Warning("owner resource not found for owned object: '%s' in namespace %s, skip updating owner CR",
					ownerRes, ownerResNamespace)
				continue
			}
			glog.Infof("Update owner %s/%s resources found for source %s/%s",
				ownerResKind, ownerResName,
				sourceResKind, sourceResName)
			ownerCR, err := kubernetes.Toolbox.GetResourceWithObjectReference(ownerObj)
			if err != nil {
				return fmt.Errorf("failed to get owner CR for owner object %s in namespace %s: %v", ownerRes, ownerResNamespace, err)
			}
			// get the new resource value from the source obj
			newCRValue, found, err := ormutils.NestedField(updatedControllerObj.Object, ownedPath)
			if err != nil || !found {
				return fmt.Errorf("failed to get value for source/owned resource %s for path '%s' in updatedControllerObj, error: %v", sourceRes, ownerPath, err)
			}
			// get the original resource value from the owner obj
			origCRValue, found, err := ormutils.NestedField(ownerCR.Object, ownerPath)
			if err != nil || !found {
				return fmt.Errorf("failed to get value for owner resource %s from path '%s' in ownerCR, error: %v", ownerRes, ownerPath, err)
			}
			// set new resource values to owenr cr obj
			if err := ormutils.SetNestedField(ownerCR.Object, newCRValue, ownerPath); err != nil {
				return fmt.Errorf("failed to set new value %v to owner CR %s '%s' in namespace %s: %v",
					newCRValue, ownerRes, ownerPath, ownerResNamespace, err)
			}
			glog.V(4).Infof("updating owner resource for owner object %s in namespace %s at owner path %s", ownerRes, ownerObj.Namespace, ownerPath)
			// update the owner cr object with new values set
			err = kubernetes.Toolbox.UpdateResourceWithGVK(ownerCR.GroupVersionKind(), ownerCR)
			if err != nil {
				return fmt.Errorf("failed to perform update action owner CR %s in namespace %s: %v", ownerRes, ownerResNamespace, err)
			}
			//set orm status only for owner object if this not orm V1
			if !ownerResources.isV1ORM {
				ormClient.SetORMStatusForOwner(ownerCR, nil)
			}
			updated = true
			glog.V(4).Infof("successfully updated owner CR %s for path '%s' from %v to %v in namespace %s", ownerRes, ownerPath, origCRValue, newCRValue, ownerResNamespace)
		}
	}
	// If updated is false at this stage, it means there are some changes turbo server is recommending to make but not
	// defined in the ORM resource mapping templates. In this case, the resource field may be missing to be defined in
	// ORM CR so it couldn't find any owner resource paths to update.
	// We send an action failure notification here because nothing gets changes after the action execution.
	if !updated {
		return fmt.Errorf("failed to update owner CR %s in namespace %s, missing owner resource", operatorRes, resourceNamespace)
	}
	return nil
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
