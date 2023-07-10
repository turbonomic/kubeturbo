module github.com/turbonomic/kubeturbo

go 1.20 //Make sure you sync the change to the file Dockerfile.multi-archs after you update the version here

require (
	github.com/golang/glog v1.0.0
	github.com/google/cadvisor v0.45.0
	github.com/mitchellh/hashstructure v0.0.0-20170609045927-2bca23e0e452
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.24.1
	github.com/opencontainers/go-digest v1.0.0
	github.com/openshift/api v0.0.0-20210816181336-8ff39b776da3
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pborman/uuid v1.2.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.14.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.8.1
	github.com/turbonomic/orm v0.0.0-20230515224524-8f968dcc8f2e
	github.com/turbonomic/turbo-gitops v0.0.0-20221208150810-105a2d5244b3
	github.com/turbonomic/turbo-go-sdk v0.0.0-20230710083128-36d2c50585d7
	github.com/turbonomic/turbo-policy v0.0.0-20230328195608-0556e3cbe9b3
	github.com/xanzy/go-gitlab v0.74.0
	k8s.io/klog/v2 v2.80.1
	k8s.io/utils v0.0.0-20221128185143-99ec85e7a448
)

require (
	github.com/avast/retry-go v3.0.0+incompatible // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver v3.5.1+incompatible // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/cilium/ebpf v0.7.0 // indirect
	github.com/containerd/cgroups v1.0.4 // indirect
	github.com/coreos/go-systemd/v22 v22.3.2 // indirect
	github.com/docker/distribution v2.8.2+incompatible // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/emicklei/go-restful/v3 v3.10.2 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/godbus/dbus/v5 v5.0.6 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/googleapis/gnostic v0.5.5 // indirect
	github.com/gorilla/websocket v1.4.2 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.1 // indirect
	github.com/imdario/mergo v0.3.12 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/nxadm/tail v1.4.8 // indirect
	github.com/opencontainers/runtime-spec v1.0.3-0.20210326190908-1c3f411f0417 // indirect
	github.com/openshift/cluster-api-provider-gcp v0.0.1-0.20210910150352-3570511c0044 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/procfs v0.8.0 // indirect
	github.com/sirupsen/logrus v1.8.1 // indirect
	github.com/turbonomic/turbo-api v0.0.0-20230707140005-7608899ba463 // indirect
	golang.org/x/crypto v0.0.0-20220315160706-3147a52a75dd // indirect
	golang.org/x/net v0.8.0 // indirect
	golang.org/x/sys v0.6.0 // indirect
	golang.org/x/term v0.6.0 // indirect
	golang.org/x/text v0.8.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.2.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/protobuf v1.28.1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/kube-openapi v0.0.0-20221012153701-172d655c2280 // indirect
	k8s.io/kubernetes v1.25.11 // indirect
	sigs.k8s.io/cluster-api-provider-aws v0.0.0-00010101000000-000000000000 // indirect
	sigs.k8s.io/cluster-api-provider-azure v0.0.0-00010101000000-000000000000 // indirect
	sigs.k8s.io/json v0.0.0-20220713155537-f223a00ba0e2 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
)

// k8s and relevant dependencies
require (
	k8s.io/api v0.26.1
	k8s.io/apiextensions-apiserver v0.26.1
	k8s.io/apimachinery v0.26.1
	k8s.io/apiserver v0.25.4
	k8s.io/client-go v0.26.1
	k8s.io/component-base v0.26.1
	k8s.io/klog v1.0.0
	k8s.io/kubelet v0.23.5
	sigs.k8s.io/application v0.8.3
)

require (
	github.com/KimMachineGun/automemlimit v0.2.4
	github.com/argoproj/gitops-engine v0.7.0
	github.com/davecgh/go-spew v1.1.1
	github.com/deckarep/golang-set v1.7.1
	github.com/evanphx/json-patch v5.6.0+incompatible
	github.com/google/go-cmp v0.5.9
	github.com/google/go-github/v42 v42.0.0
	github.com/openshift/client-go v0.0.0-20210730113412-1811c1b3fc0e
	// openshift cluster api for cluster-api based node provision and suspend
	// TODO (fix this): There is an observed problem here:
	// The dependencies of machine-api-operator use cgo (import "C"). For some reason
	// the 'go get' dependencies does not cleanly complete, so the version here is updated manually.
	// The hash maps to release release-4.10
	github.com/openshift/machine-api-operator v0.2.1-0.20210923190431-734dcea054a1
	github.com/prometheus/client_model v0.3.0
	github.com/prometheus/common v0.37.0
	golang.org/x/oauth2 v0.0.0-20221014153046-6fdb5e3db783
	gopkg.in/yaml.v3 v3.0.1 // indirect
	sigs.k8s.io/controller-runtime v0.14.5
	sigs.k8s.io/yaml v1.3.0
)

replace (
	golang.org/x/crypto => golang.org/x/crypto v0.0.0-20220427172511-eb4f295cb31f
	sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20201125052318-b85a18cbf338
	sigs.k8s.io/cluster-api-provider-azure => github.com/openshift/cluster-api-provider-azure v0.0.0-20210209143830-3442c7a36c1e
)

replace (
	// https://github.com/golang/go/issues/33546#issuecomment-519656923
	github.com/go-check/check => github.com/go-check/check v0.0.0-20180628173108-788fd7840127

	k8s.io/api => k8s.io/api v0.25.4
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.23.5
	k8s.io/apimachinery => k8s.io/apimachinery v0.23.5
	k8s.io/apiserver => k8s.io/apiserver v0.25.4
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.23.5
	k8s.io/client-go => k8s.io/client-go v0.23.5
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.23.5
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.23.5
	k8s.io/code-generator => k8s.io/code-generator v0.23.5
	k8s.io/component-base => k8s.io/component-base v0.23.5
	k8s.io/component-helpers => k8s.io/component-helpers v0.23.5
	k8s.io/controller-manager => k8s.io/controller-manager v0.23.5
	k8s.io/cri-api => k8s.io/cri-api v0.23.5
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.23.5
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.23.5
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.23.5
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.23.5
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.23.5
	k8s.io/kubectl => k8s.io/kubectl v0.23.5
	k8s.io/kubelet => k8s.io/kubelet v0.23.5
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.23.5
	k8s.io/metrics => k8s.io/metrics v0.23.5
	k8s.io/mount-utils => k8s.io/mount-utils v0.23.5
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.23.5
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.23.5
)

replace (
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20211115234752-e816edb12b65
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.11.0
)
