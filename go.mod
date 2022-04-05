module github.com/turbonomic/kubeturbo

go 1.16

require (
	github.com/golang/glog v1.0.0
	github.com/google/cadvisor v0.39.2
	github.com/mitchellh/hashstructure v0.0.0-20170609045927-2bca23e0e452
	github.com/onsi/ginkgo v1.16.4
	github.com/onsi/gomega v1.14.0
	github.com/opencontainers/go-digest v1.0.0
	github.com/openshift/api v0.0.0-20210816181336-8ff39b776da3
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pborman/uuid v1.2.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.11.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.0
	github.com/turbonomic/turbo-go-sdk v0.0.0-20220405155252-eb6acce31600
)

// k8s and relevant dependencies
require (
	k8s.io/api v0.22.2
	k8s.io/apiextensions-apiserver v0.22.2
	k8s.io/apimachinery v0.22.2
	k8s.io/apiserver v0.22.2
	k8s.io/client-go v0.22.2
	k8s.io/component-base v0.22.2
	k8s.io/klog v1.0.0
	k8s.io/kubelet v0.22.2
	sigs.k8s.io/application v0.8.3
)

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/deckarep/golang-set v1.7.1
	github.com/openshift/client-go v0.0.0-20210730113412-1811c1b3fc0e
	// openshift cluster api for cluster-api based node provision and suspend
	// TODO (fix this): There is an observed problem here:
	// The dependencies of machine-api-operator use cgo (import "C"). For some reason
	// the 'go get' dependencies does not cleanly complete, so the version here is updated manually.
	// The hash maps to release release-4.10
	github.com/openshift/machine-api-operator v0.2.1-0.20210923190431-734dcea054a1
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.28.0
)

replace (
	sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20201125052318-b85a18cbf338
	sigs.k8s.io/cluster-api-provider-azure => github.com/openshift/cluster-api-provider-azure v0.0.0-20210209143830-3442c7a36c1e
)
