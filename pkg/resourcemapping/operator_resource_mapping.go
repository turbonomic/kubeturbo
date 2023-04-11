package resourcemapping

import (
	"bytes"
	"context"
	"fmt"
	devopsv1alpha1 "github.com/turbonomic/orm/api/v1alpha1"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"text/template"

	"github.com/golang/glog"
	discoveryutil "github.com/turbonomic/kubeturbo/pkg/discovery/util"
	"github.com/turbonomic/kubeturbo/pkg/util"
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

func (ormClient *ORMClient) getOperatorCRsFromCRD(crdName, namespace string) ([]unstructured.Unstructured, schema.GroupVersionResource, bool, error) {
	// First get the Operator CRD
	crds := ormClient.apiExtClient.CustomResourceDefinitions() //RESTClient is used to communicate
	// with API server by this client implementation.
	crd, err := crds.Get(context.TODO(), crdName, metav1.GetOptions{})
	var isClusterScope bool
	if err != nil {
		return nil, schema.GroupVersionResource{}, isClusterScope, err
	}
	groupVersionRes := schema.GroupVersionResource{
		Group:    crd.Spec.Group,
		Version:  crd.Spec.Versions[0].Name,
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

func (ormClient *ORMClient) retrieveORM(ownedObj *unstructured.Unstructured, ownerReference discoveryutil.OwnerInfo) (*ORMSpec, *unstructured.Unstructured, error) {
	operatorCRUID := string(ownerReference.Uid)               // owner is considered as operator
	ownedKey := ownedObj.GetKind() + "/" + ownedObj.GetName() // source or owned resource

	operatorResourceSpec, exists := ormClient.operatorResourceSpecMap[operatorCRUID] //orm for the operator/owner
	if !exists {
		return nil, nil, fmt.Errorf("operatorResourceSpec not found in operatorResourceSpecMap for operatorCR %s", operatorCRUID)
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
		return nil, nil, fmt.Errorf("failed to get CR %s for %s in namespace %s: %v",
			operatorRes, ownedKey, resourceNamespace, err)
	}

	return operatorResourceSpec, operatorCR, nil
}

func (ormClient *ORMClient) LocateOwnerPaths(ownedObj *unstructured.Unstructured, ownerReference discoveryutil.OwnerInfo, owned []*devopsv1alpha1.ResourcePath) (map[string][]devopsv1alpha1.ResourcePath, error) {
	ownedKey := ownedObj.GetKind() + "/" + ownedObj.GetName() // source or owned resource

	var allOwnerResourcePaths map[string][]devopsv1alpha1.ResourcePath

	orm, ownerObj, err := ormClient.retrieveORM(ownedObj, ownerReference)
	if err != nil {
		return allOwnerResourcePaths, err
	}
	if orm == nil {
		return allOwnerResourcePaths, fmt.Errorf("Cannot find orm V1 for source: %s\n", ownedKey)
	}
	if orm == nil || ownerObj == nil {
		return allOwnerResourcePaths, fmt.Errorf("Cannot find owner for source: %s\n", ownedKey)
	}

	var ownerRef v1.ObjectReference
	ownerRef = v1.ObjectReference{
		Kind:       ownerObj.GetKind(),
		Namespace:  ownerObj.GetNamespace(),
		Name:       ownerObj.GetName(),
		APIVersion: ownerObj.GetAPIVersion(),
	}

	ormTemplate, exists := orm.ormTemplateMap[ownedKey] //path mappings for the source
	if !exists {
		return nil, fmt.Errorf("ormTemplate not found in ormTemplateMap for componentKey %s for operatorCR %s",
			ownedKey, ownerObj.GetUID())
	}

	operatorRes := ownerObj.GetKind() + "/" + ownerObj.GetName()
	resourceNamespace := ownedObj.GetNamespace()

	owners := map[string][]devopsv1alpha1.ResourcePath{}
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

// Update updates the corresponding CR for an Operator manged resource based on OperatorResourceMapping
// origControllerObj -- original K8s controller object
// updatedControllerObj -- updated K8s controller object based on Turbo actionItem, from which the resource value is fetched
//
//	and will be set to the corresponding CR
//
// controllerOwnerReference -- ownerReference of a K8s controller, which contains metadata of a Operator CR
func (ormClient *ORMClient) Update(origControllerObj, updatedControllerObj *unstructured.Unstructured, controllerOwnerReference discoveryutil.OwnerInfo) error {
	operatorCRUID := string(controllerOwnerReference.Uid)                                 // owner is considered as operator
	componentKey := updatedControllerObj.GetKind() + "/" + updatedControllerObj.GetName() // source or owned resource
	ormClient.cacheLock.Lock()
	defer ormClient.cacheLock.Unlock()
	glog.Infof("Update owner %s/%s resources found using orm v1 for source %s/%s",
		controllerOwnerReference.Kind, controllerOwnerReference.Name,
		updatedControllerObj.GetKind(), updatedControllerObj.GetName())

	operatorResourceSpec, exists := ormClient.operatorResourceSpecMap[operatorCRUID]
	if !exists {
		return fmt.Errorf("operatorResourceSpec not found in operatorResourceSpecMap for operatorCR %s", operatorCRUID)
	}
	ormTemplate, exists := operatorResourceSpec.ormTemplateMap[componentKey]
	if !exists {
		return fmt.Errorf("ormTemplate not found in ormTemplateMap for componentKey %s for operatorCR %s", componentKey, operatorCRUID)
	}

	operatorResKind := controllerOwnerReference.Kind //operator kind and instance
	operatorResName := controllerOwnerReference.Name
	operatorRes := operatorResKind + "/" + operatorResName

	resourceNamespace := updatedControllerObj.GetNamespace()
	if operatorResourceSpec.isClusterScope {
		resourceNamespace = v1.NamespaceAll
	}
	dynResourceClient := ormClient.dynClient.Resource(operatorResourceSpec.operatorGVResource).Namespace(resourceNamespace)
	operatorCR, err := dynResourceClient.Get(context.TODO(), operatorResName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get CR %s for %s in namespace %s: %v", operatorRes, componentKey, resourceNamespace, err)
	}

	glog.V(2).Infof("Updating CR %s for %s in namespace %s", operatorRes, componentKey, resourceNamespace)
	updated := false
	// Update based on each resourceMappingTemplate
	for _, resourceMappingTemplate := range ormTemplate.resourceMappingTemplates {
		// Set resourceMappingComponentName to resourceMappingTemplate so as to parse the srcPath and destPath based on text template
		resourceMappingTemplate[resourceMappingComponentName] = ormTemplate.componentName
		srcPath, destPath, err := ormClient.parseSrcAndDestPath(resourceMappingTemplate)
		if err != nil {
			return fmt.Errorf("failed to update CR %s for %s in namespace %s: error while parsing resource paths [source %v, dest %v]:%v",
				operatorRes, componentKey, resourceNamespace, srcPath, destPath, err)
		}
		glog.V(4).Infof("Source path: %v, Dest path: %v", srcPath, destPath)

		srcPathExists, err := ormClient.checkResourcePath(origControllerObj, srcPath)
		if !srcPathExists {
			glog.Warningf("source path %s does not exist: %v\n", srcPath, err)
			continue
		}
		// Check if CR needs update from current resourceMappingTemplate. If the values are same under the same srcPath
		// from origControllerObj and updatedControllerObj, it means resource value is not changed and no need to update
		// corresponding destPath in CR.
		newValue, needsUpdate, err := ormClient.needsUpdate(origControllerObj, updatedControllerObj, srcPath)
		if err != nil {
			return fmt.Errorf("failed to update CR %s for %s in namespace %s: %v", operatorRes, componentKey, resourceNamespace, err)
		}
		if !needsUpdate {
			glog.V(4).Infof("No changes in controller %s/%s src path '%s'. Skip updating Operator CR %s.",
				updatedControllerObj.GetKind(), updatedControllerObj.GetName(), srcPath, operatorRes)
			continue
		}
		re := regexp.MustCompile(`(\[)|(\]\.)`)
		parsedDestPath := re.ReplaceAllString(destPath, ".")
		fields := strings.Split(parsedDestPath, ".")
		if len(fields) < 2 {
			return fmt.Errorf("failed to update %v to CR %s for %s in namespace %s: '%s' is invalid path", newValue, operatorRes, componentKey, resourceNamespace, destPath)
		}
		origCRValue, _, err := util.NestedField(operatorCR, resourceMappingDestPath, destPath)
		if err != nil {
			return fmt.Errorf("failed to update %v to CR %s '%s' for %s in namespace %s: %v", newValue, operatorRes, destPath, componentKey, resourceNamespace, err)
		}
		err = util.SetNestedField(operatorCR.Object, newValue, fields[1:]...)
		if err != nil {
			return fmt.Errorf("failed to update %v to CR %s '%s' for %s in namespace %s: %v", newValue, operatorRes, destPath, componentKey, resourceNamespace, err)
		}
		_, err = dynResourceClient.Update(context.TODO(), operatorCR, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update %v to CR %s '%s' for %s in namespace %s: %v", newValue, operatorRes, destPath, componentKey, resourceNamespace, err)
		}
		updated = true
		glog.Infof("Successfully updated CR %s '%s' from %v to %v for %s in namespace %s", operatorRes, destPath, origCRValue, newValue, componentKey, resourceNamespace)
	}
	// If needsUpdate is false at this stage, it means there are some changes turbo server is recommending to make but not
	// defined in the ORM resource mapping templates. In this case, either the resource field is missing to be defined in
	// ORM CR or the field is not allowed to be changed, like a fixed value managed by Operator. We send an action failure
	// notification here because nothing gets changes after the action execution.
	if !updated {
		return fmt.Errorf("failed to update CR %s for %s in namespace %s: missing resource mapping template", operatorRes, componentKey, resourceNamespace)
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

// needsUpdate returns false if values from origControllerObj and updatedControllerObj are the same for the same srcPath.
func (ormClient *ORMClient) needsUpdate(origControllerObj, updatedControllerObj *unstructured.Unstructured, srcPath string) (interface{}, bool, error) {
	oldVal, found, err := util.NestedField(origControllerObj, resourceMappingSrcPath, srcPath)
	if err != nil || !found {
		return nil, false, fmt.Errorf("failed to get value from path '%s' in origControllerObj, error: %v", srcPath, err)
	}
	newVal, found, err := util.NestedField(updatedControllerObj, resourceMappingSrcPath, srcPath)
	if err != nil || !found {
		return nil, false, fmt.Errorf("failed to get value from path '%s' in updatedControllerObj, error: %v", srcPath, err)
	}
	if reflect.DeepEqual(oldVal, newVal) {
		return nil, false, nil
	}
	return newVal, true, nil
}

// needsUpdate returns false if values from origControllerObj and updatedControllerObj are the same for the same srcPath.
func (ormClient *ORMClient) checkResourcePath(resourceObj *unstructured.Unstructured, path string) (bool, error) {
	_, found, err := util.NestedField(resourceObj, resourceMappingSrcPath, path)
	if err != nil || !found {
		return false, fmt.Errorf("failed to get value from path '%s' in resourceObj: %s/%s, error -> %v\n",
			path, resourceObj.GetKind(), resourceObj.GetName(), err)
	}

	return true, nil
}
