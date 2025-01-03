package util

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
)

const QuotaAnnotationKey = "kubeturbo.io/last-good-config"

func getCodecForGV(gv schema.GroupVersion) (runtime.Codec, error) {
	scheme := runtime.NewScheme()
	err := v1.AddToScheme(scheme)
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

func CopyQuota(quota *v1.ResourceQuota) *v1.ResourceQuota {
	newQuota := &v1.ResourceQuota{}
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

func EncodeQuota(quota *v1.ResourceQuota) (string, error) {
	encoder, err := getCodecForGV(schema.GroupVersion{Group: "", Version: "v1"})
	if err != nil {
		return "", err
	}

	quotaCopy := CopyQuota(quota)
	bytes, err := runtime.Encode(encoder, quotaCopy)
	if err != nil {
		return "", err
	}

	return string(bytes), nil
}

func DecodeQuota(bytes []byte) (*v1.ResourceQuota, error) {
	decode := scheme.Codecs.UniversalDeserializer().Decode
	obj, _, err := decode(bytes, nil, nil)
	if err != nil {
		return nil, err
	}

	quota, isQuota := obj.(*v1.ResourceQuota)
	if !isQuota {
		return nil, fmt.Errorf("could not get the quota spec from the supplied json")
	}

	return quota, nil
}
