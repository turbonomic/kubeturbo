module github.com/turbonomic/kubeturbo

go 1.16

require (
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/google/cadvisor v0.39.0
	github.com/mitchellh/hashstructure v0.0.0-20170609045927-2bca23e0e452
	github.com/onsi/ginkgo v1.15.0
	github.com/onsi/gomega v1.10.5
	github.com/opencontainers/go-digest v1.0.0
	github.com/openshift/api v0.0.0-20210412212256-79bd8cfbbd59
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pborman/uuid v1.2.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.9.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.6.1
	github.com/turbonomic/turbo-go-sdk v0.0.0-20210820114323-d54aadceaf9e
)

// k8s and relevant dependencies
require (
	k8s.io/api v0.21.0
	k8s.io/apiextensions-apiserver v0.21.0
	k8s.io/apimachinery v0.21.0
	k8s.io/apiserver v0.21.0
	k8s.io/autoscaler/cluster-autoscaler v0.0.0-20210503103403-79a43dfe1954
	k8s.io/client-go v0.21.0
	k8s.io/component-base v0.21.0
	k8s.io/klog v1.0.0
	k8s.io/kubernetes v1.21.0
	sigs.k8s.io/application v0.8.3
)

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/deckarep/golang-set v1.7.1
	github.com/openshift/client-go v0.0.0-20210409155308-a8e62c60e930
	// openshift cluster api for cluster-api based node provision and suspend
	// TODO (fix this): There are two observed problems here:
	// 1. the dependencies of machine-api-operator use cgo (import "C"). For some reason
	// the go get dependencies does not cleanly complete, so the version here is uddated manually.
	// 2. The package github.com/googleapis/gnostic is picked in dependencies of both
	// machine-api-operator and cluster-autoscaler, but with different versions.
	// cluster-autoscaler and relevant k8s dependencies map to v0.4.1 whereas the machine-api-operator
	// dependencies resolve to v0.5.4. Right now build works fine with v0.5.4 as there isn't much
	// we haven't used from cluster-autoscaler yet, but will probably need a resolution for this
	// at some point.
	github.com/openshift/machine-api-operator v0.2.1-0.20210805083755-df4ed38125e9
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.15.0
	golang.org/x/net v0.0.0-20210405180319-a5a99cb37ef4 // indirect
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c // indirect
	golang.org/x/sys v0.0.0-20210510120138-977fb7262007 // indirect
	k8s.io/kubelet v0.21.0
)

replace (
	sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20201125052318-b85a18cbf338
	sigs.k8s.io/cluster-api-provider-azure => github.com/openshift/cluster-api-provider-azure v0.0.0-20210209143830-3442c7a36c1e
)

// azure sdk to match cluster autoscaler dependency
replace github.com/Azure/azure-sdk-for-go => github.com/Azure/azure-sdk-for-go v43.0.0+incompatible

// both cluster autoscaler and k8s are not designed to be vendored and use k8s
// staging dependencies from the core k/k repo. We need to mandatorily provide the
// replace directives to ensure the right package is pulled wrt k8s and cluster autoscaler
// cluster autoscaler's hash maps to k8s release 1.19.1 and CAs release "Cluster Autoscaler 1.19.1".
replace (
	k8s.io/api => k8s.io/api v0.21.0
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.21.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.21.0
	k8s.io/apiserver => k8s.io/apiserver v0.21.0
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.21.0
	k8s.io/client-go => k8s.io/client-go v0.21.0
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.21.0
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.21.0
	k8s.io/code-generator => k8s.io/code-generator v0.21.0
	k8s.io/component-base => k8s.io/component-base v0.21.0
	k8s.io/component-helpers => k8s.io/component-helpers v0.21.0
	k8s.io/controller-manager => k8s.io/controller-manager v0.21.0
	k8s.io/cri-api => k8s.io/cri-api v0.21.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.21.0
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.21.0
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.21.0
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.21.0
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.21.0
	k8s.io/kubectl => k8s.io/kubectl v0.21.0
	k8s.io/kubelet => k8s.io/kubelet v0.21.0
	k8s.io/kubernetes => k8s.io/kubernetes v1.21.0
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.21.0
	k8s.io/metrics => k8s.io/metrics v0.21.0
	k8s.io/mount-utils => k8s.io/mount-utils v0.21.0
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.21.0
)

// etcd is also moving to go mod based dependency mgmt and in that processmake
// its clean vendoring seems to be broken. This is to make etcd work.
replace google.golang.org/grpc => google.golang.org/grpc v1.27.0
replace github.com/turbonomic/turbo-go-sdk => /Users/pallavidebnath/go/src/github.com/turbonomic/turbo-go-sdk