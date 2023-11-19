package executor

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	"github.ibm.com/turbonomic/kubeturbo/pkg/action/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/clock"
	quota "k8s.io/apiserver/pkg/quota/v1"
	kclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
)

const QuotaAnnotationKey = "kubeturbo.io/last-good-config"

type QuotaAccessor interface {
	Get() ([]*corev1.ResourceQuota, error)
	Evaluate(quotas []*corev1.ResourceQuota, pod *corev1.Pod, replicas int64) error
	Update() error
	Revert()
}

type QuotaAccessorImpl struct {
	client    *kclient.Clientset
	evaluator *podEvaluator
	namespace string
	quotas    []*corev1.ResourceQuota
}

func NewQuotaAccessor(client *kclient.Clientset, namespace string) QuotaAccessor {
	return &QuotaAccessorImpl{
		client:    client,
		namespace: namespace,
		evaluator: NewPodEvaluator(nil, clock.RealClock{}),
	}
}

// GAP: Admission plugins can also configure AdmissionConfiguration limits.
// We currently won't have a way of working around that, as its a one time API
// plugin configuration. Actions in such cases would fail.

// Current update flow is as below:
// 1. get all quotas
// For each quota
// 2. identify if a quota needs update
// 3. copy the original spec out into the annotation on the quota which needs update
// 4. update the quota
// once the action finishes
// 5. revert the quota back to the original state by applying the previous spec from the annotation

func (q *QuotaAccessorImpl) Get() ([]*corev1.ResourceQuota, error) {
	quotaList, err := q.client.CoreV1().ResourceQuotas(q.namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	resourceQuotas := []*corev1.ResourceQuota{}
	if quotaList == nil {
		return resourceQuotas, nil
	}
	if len(quotaList.Items) < 1 {
		return resourceQuotas, nil
	}

	items := quotaList.Items
	for i := range items {
		quota := items[i]
		resourceQuotas = append(resourceQuotas, &quota)
	}

	return resourceQuotas, nil
}

func (q *QuotaAccessorImpl) Evaluate(quotas []*corev1.ResourceQuota, pod *corev1.Pod, replicas int64) error {
	podQualifiedName := fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
	quotasToUpdate := []*corev1.ResourceQuota{}
	var totalDeltaUsage corev1.ResourceList
	deltaUsage, err := q.evaluator.Usage(pod)
	if err != nil {
		return err
	}

	// ignore items in deltaUsage with zero usage
	deltaUsage = removeZeros(deltaUsage)

	for i := int64(0); i < replicas; i++ {
		totalDeltaUsage = quota.Add(totalDeltaUsage, deltaUsage)
	}
	// if there is no remaining non-zero usage, short-circuit and return
	if len(totalDeltaUsage) == 0 {
		return nil
	}

	for i := range quotas {
		resourceQuota := quotas[i]

		match, err := q.evaluator.Matches(resourceQuota, pod)
		if err != nil {
			glog.Warningf("Error occurred while matching resource quota, %v, against pod %s. Err: %v", resourceQuota, podQualifiedName, err)
			return err
		}
		if !match {
			continue
		}

		hardResources := quota.ResourceNames(resourceQuota.Status.Hard)
		restrictedResources := q.evaluator.MatchingResources(hardResources)

		if !hasUsageStats(resourceQuota, restrictedResources) {
			// There being no usage stats for this quota would mean that its
			// allright to be going ahead with trying to move the pod.
			// We continue to check other quotas
			glog.Warningf("No usage stats found on resource quota: %s, while validating quota for pod: %s.", resourceQuota.Name, podQualifiedName)
			continue
		}

		requestedUsage := quota.Mask(totalDeltaUsage, hardResources)
		newTotalUsage := quota.Add(resourceQuota.Status.Used, requestedUsage)
		// We need to mask again as add returns all resources in the first list
		maskedNewTotalUsage := quota.Mask(newTotalUsage, quota.ResourceNames(requestedUsage))

		if allowed, exceededResources := quota.LessThanOrEqual(maskedNewTotalUsage, resourceQuota.Status.Hard); !allowed {
			// Add the original quota spec to the annotation on the quota to update
			newQuota := copyQuota(resourceQuota)
			newQuota.Spec.Hard = AddToSpecHard(resourceQuota.Spec.Hard, requestedUsage, exceededResources)
			if newQuota.Annotations == nil {
				newQuota.Annotations = make(map[string]string)
			}

			// As we don't have any other mechanism to have a persistent storage
			// We update the original spec as an annotation on the quota itself
			// Updating the quota this way also facilitates the garbage collection
			newQuota.Annotations[QuotaAnnotationKey], err = EncodeQuota(resourceQuota)
			if err != nil {
				// we don't skip this error. It can any ways be problematic if we needed
				// to update multiple quotas but could not update 1 of them. The action
				// would still fail.
				return fmt.Errorf("Error updating resource quota annotations: %v", err)
			}
			addGCLabelOnQuota(newQuota)
			quotasToUpdate = append(quotasToUpdate, newQuota)
		}
	}

	q.quotas = quotasToUpdate
	return nil
}

func (q *QuotaAccessorImpl) Update() error {
	for _, quota := range q.quotas {
		_, err := q.client.CoreV1().ResourceQuotas(q.namespace).Update(context.TODO(), quota, metav1.UpdateOptions{})
		if err != nil {
			// If an update fails we don't continue
			return err
		}
	}
	return nil
}

func (q *QuotaAccessorImpl) Revert() {
	for _, quota := range q.quotas {
		var revertedQuota *corev1.ResourceQuota
		var err error
		if quota.Annotations != nil {
			origSpec, exists := quota.Annotations[QuotaAnnotationKey]
			if exists {
				revertedQuota, err = DecodeQuota([]byte(origSpec))
				if err != nil {
					glog.Warningf("Error reverting the updated quota: %s/%s: %v", quota.Namespace, quota.Name, err)
					continue
				}
			}
		}
		if revertedQuota == nil {
			continue
		}

		RemoveGCLabelFromQuota(revertedQuota)
		_, err = q.client.CoreV1().ResourceQuotas(q.namespace).Update(context.TODO(), revertedQuota, metav1.UpdateOptions{})
		if err != nil {
			glog.Warningf("Error reverting the updated quota: %s/%s: %v", quota.Namespace, quota.Name, err)
			continue
		}
	}
}

// hasUsageStats returns true if for atleast 1 hard constraint in interestingResources there is a value for its current usage
func hasUsageStats(resourceQuota *corev1.ResourceQuota, interestingResources []corev1.ResourceName) bool {
	interestingSet := quota.ToSet(interestingResources)
	for resourceName := range resourceQuota.Status.Hard {
		if !interestingSet.Has(string(resourceName)) {
			continue
		}
		if _, found := resourceQuota.Status.Used[resourceName]; found {
			return true
		}
	}

	return false
}

// RemoveZeros returns a new resource list that only has no zero values
func removeZeros(a corev1.ResourceList) corev1.ResourceList {
	result := corev1.ResourceList{}
	for key, value := range a {
		if !value.IsZero() {
			result[key] = value
		}
	}
	return result
}

// Add adds the delta to the spec only for the given keys
func AddToSpecHard(a corev1.ResourceList, b corev1.ResourceList, keys []corev1.ResourceName) corev1.ResourceList {
	result := corev1.ResourceList{}
	for key, value := range a {
		quantity := value.DeepCopy()
		if other, found := b[key]; found && inList(key, keys) {
			quantity.Add(other)
		}
		result[key] = quantity
	}

	return result
}

func inList(k corev1.ResourceName, keys []corev1.ResourceName) bool {
	for _, key := range keys {
		if k == key {
			return true
		}
	}
	return false
}

func DecodeQuota(bytes []byte) (*corev1.ResourceQuota, error) {
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode(bytes, nil, nil)
	if err != nil {
		return nil, err
	}

	quota, isQuota := obj.(*corev1.ResourceQuota)
	if !isQuota {
		return nil, fmt.Errorf("could not get the quota spec from the supplied json.")
	}

	return quota, nil
}

func EncodeQuota(quota *corev1.ResourceQuota) (string, error) {
	encoder, err := getCodecForGV(schema.GroupVersion{Group: "", Version: "v1"})
	if err != nil {
		return "", err
	}

	quotaCopy := copyQuota(quota)
	bytes, err := runtime.Encode(encoder, quotaCopy)
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

func copyQuota(quota *corev1.ResourceQuota) *corev1.ResourceQuota {
	newQuota := &corev1.ResourceQuota{}
	newQuota.TypeMeta = quota.TypeMeta
	newQuota.ObjectMeta = quota.ObjectMeta
	newQuota.UID = ""
	newQuota.SelfLink = ""
	newQuota.ResourceVersion = ""
	newQuota.Generation = 0
	newQuota.CreationTimestamp = metav1.Time{}
	newQuota.DeletionTimestamp = nil
	newQuota.DeletionGracePeriodSeconds = nil
	newQuota.ManagedFields = nil
	newQuota.Spec = quota.Spec
	return newQuota
}

func getCodecForGV(gv schema.GroupVersion) (runtime.Codec, error) {
	scheme := runtime.NewScheme()
	err := corev1.AddToScheme(scheme)
	if err != nil {
		return nil, err
	}

	codecs := serializer.NewCodecFactory(scheme)
	mediaType := runtime.ContentTypeJSON
	serializerInfo, ok := runtime.SerializerInfoForMediaType(codecs.SupportedMediaTypes(), mediaType)
	if !ok {
		return nil, fmt.Errorf("unable to locate encoder -- %q is not a supported media type", mediaType)
	}
	codec := codecs.CodecForVersions(serializerInfo.Serializer, codecs.UniversalDeserializer(), gv, nil)
	return codec, nil
}

func addGCLabelOnQuota(quota *corev1.ResourceQuota) {
	labels := quota.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[TurboGCLabelKey] = TurboGCLabelVal

	quota.SetLabels(labels)
}

func RemoveGCLabelFromQuota(quota *corev1.ResourceQuota) {
	labels := quota.GetLabels()
	if labels == nil {
		// nothing to do
		return
	}
	if _, exists := labels[TurboGCLabelKey]; exists {
		delete(labels, TurboGCLabelKey)
		quota.SetLabels(labels)
	}
}

// checkQuotas checks and updates a quota if need be in the pods namespace
func checkQuotas(quotaAccessor QuotaAccessor, pod *corev1.Pod, lockMap *util.ExpirationMap, replicas int64) error {
	quotas, err := quotaAccessor.Get()
	if err != nil {
		return err
	}
	if err := quotaAccessor.Evaluate(quotas, pod, replicas); err != nil {
		return err
	}

	if err := quotaAccessor.Update(); err != nil {
		return err
	}

	return nil
}

func lockForQuota(namespace string, lockMap *util.ExpirationMap) (*util.LockHelper, error) {
	quotaLockKey := fmt.Sprintf("quota-lock-%s", namespace)
	lockHelper, err := util.NewLockHelper(quotaLockKey, lockMap)
	if err != nil {
		return nil, err
	}

	err = lockHelper.Trylock(defaultWaitLockTimeOut, defaultWaitLockSleep)
	if err != nil {
		glog.Errorf("Failed to acquire lock with key(%v): %v", quotaLockKey, err)
		return nil, err
	}
	lockHelper.KeepRenewLock()

	return lockHelper, nil
}
