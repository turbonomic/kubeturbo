module github.com/turbonomic/kubeturbo

go 1.13

require (
	github.com/go-logr/zapr v0.1.1 // indirect
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/google/cadvisor v0.38.6
	github.com/mitchellh/hashstructure v0.0.0-20170609045927-2bca23e0e452
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.1
	github.com/opencontainers/go-digest v1.0.0
	github.com/openshift/api v0.0.0-20201019163320-c6a5ec25f267
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pborman/uuid v1.2.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.7.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.6.1
	github.com/turbonomic/turbo-go-sdk v0.0.0-20210513033620-9b641b508d10
)

// k8s and relevant dependencies
require (
	k8s.io/api v0.19.4
	k8s.io/apiextensions-apiserver v0.19.2
	k8s.io/apimachinery v0.19.4
	k8s.io/apiserver v0.19.2
	k8s.io/autoscaler/cluster-autoscaler v0.0.0-20201102145256-1ba66f5cf714
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/component-base v0.19.2
	k8s.io/klog v1.0.0
	k8s.io/kubernetes v1.19.1
	sigs.k8s.io/application v0.8.3
)

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/openshift/client-go v0.0.0-20201020074620-f8fd44879f7c
	// openshift cluster api for cluster-api based node provision and suspend
	github.com/openshift/machine-api-operator v0.2.1-0.20201216110516-d9e48bb9fc0b
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.10.0
	k8s.io/kubelet v0.0.0
)

replace (
	sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20201125052318-b85a18cbf338
	sigs.k8s.io/cluster-api-provider-azure => github.com/openshift/cluster-api-provider-azure v0.1.0-alpha.3.0.20201130182513-88b90230f2a4
)

// azure sdk to match cluster autoscaler dependency
replace github.com/Azure/azure-sdk-for-go => github.com/Azure/azure-sdk-for-go v43.0.0+incompatible

// both cluster autoscaler and k8s are not designed to be vendored and use k8s
// staging dependencies from the core k/k repo. We need to mandatorily provide the
// replace directives to ensure the right package is pulled wrt k8s and cluster autoscaler
// cluster autoscaler's hash maps to k8s release 1.19.1 and CAs release "Cluster Autoscaler 1.19.1".
replace (
	k8s.io/api => k8s.io/api v0.19.1
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.19.1
	k8s.io/apimachinery => k8s.io/apimachinery v0.19.1
	k8s.io/apiserver => k8s.io/apiserver v0.19.1
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.19.1
	k8s.io/client-go => k8s.io/client-go v0.19.1
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.19.1
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.19.1
	k8s.io/code-generator => k8s.io/code-generator v0.19.1
	k8s.io/component-base => k8s.io/component-base v0.19.1
	k8s.io/component-helpers => k8s.io/component-helpers v0.19.1
	k8s.io/controller-manager => k8s.io/controller-manager v0.19.1
	k8s.io/cri-api => k8s.io/cri-api v0.19.1
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.19.1
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.19.1
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.19.1
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.19.1
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.19.1
	k8s.io/kubectl => k8s.io/kubectl v0.19.1
	k8s.io/kubelet => k8s.io/kubelet v0.19.1
	k8s.io/kubernetes => k8s.io/kubernetes v1.19.1
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.19.1
	k8s.io/metrics => k8s.io/metrics v0.19.1
	k8s.io/mount-utils => k8s.io/mount-utils v0.19.1
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.19.1
)

// etcd is also moving to go mod based dependency mgmt and in that process
// its clean vendoring seems to be broken. This is to make etcd work.
replace google.golang.org/grpc => google.golang.org/grpc v1.27.0
