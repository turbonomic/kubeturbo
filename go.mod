module github.ibm.com/turbonomic/kubeturbo

go 1.21 //Make sure you sync the change to the file Dockerfile.multi-archs after you update the version here

require (
	github.com/fsnotify/fsnotify v1.7.0
	github.com/golang/glog v1.0.0
	github.com/google/cadvisor v0.48.1
	github.com/mitchellh/hashstructure v0.0.0-20170609045927-2bca23e0e452
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.29.0
	github.com/opencontainers/go-digest v1.0.0
	github.com/openshift/api v0.0.0-20231204192004-bfea29e5e6c4
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pborman/uuid v1.2.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.16.0
	github.com/spf13/pflag v1.0.5
	github.com/spf13/viper v1.16.0
	github.com/stretchr/testify v1.8.4
	github.com/xanzy/go-gitlab v0.74.0
	github.ibm.com/turbonomic/orm v0.0.0-20240130181339-98ec3af7e306
	github.ibm.com/turbonomic/turbo-gitops v0.0.0-20230919200933-5599d8445b5a
	github.ibm.com/turbonomic/turbo-go-sdk v0.0.0-20240802160427-5d2c139f760b
	github.ibm.com/turbonomic/turbo-policy v0.0.0-20240708101931-d7d793451ce7
	k8s.io/klog/v2 v2.110.1
	k8s.io/utils v0.0.0-20230726121419-3b25d923346b
)

require (
	github.com/avast/retry-go v3.0.0+incompatible // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/cilium/ebpf v0.11.0 // indirect
	github.com/containerd/cgroups/v3 v3.0.2 // indirect
	github.com/coreos/go-systemd/v22 v22.5.0 // indirect
	github.com/distribution/reference v0.5.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/emicklei/go-restful/v3 v3.11.0 // indirect
	github.com/evanphx/json-patch/v5 v5.6.0 // indirect
	github.com/go-logr/logr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.7 // indirect
	github.com/hashicorp/hcl v1.0.0 // indirect
	github.com/imdario/mergo v0.3.13 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/magiconair/properties v1.8.7 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nxadm/tail v1.4.8 // indirect
	github.com/opencontainers/runtime-spec v1.1.0 // indirect
	github.com/pelletier/go-toml/v2 v2.0.8 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/procfs v0.10.1 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/spf13/afero v1.9.5 // indirect
	github.com/spf13/cast v1.5.1 // indirect
	github.com/spf13/cobra v1.7.0 // indirect
	github.com/spf13/jwalterweatherman v1.1.0 // indirect
	github.com/subosito/gotenv v1.4.2 // indirect
	github.ibm.com/turbonomic/turbo-api v0.0.0-20240802153844-385076971c7b // indirect
	golang.org/x/crypto v0.21.0 // indirect
	golang.org/x/exp v0.0.0-20230817173708-d852ddb80c63 // indirect
	golang.org/x/net v0.23.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/term v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	golang.org/x/time v0.3.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.3.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/kube-openapi v0.0.0-20231010175941-2dd684a91f00 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
)

// k8s and relevant dependencies
require (
	k8s.io/api v0.29.4
	k8s.io/apiextensions-apiserver v0.29.4
	k8s.io/apimachinery v0.29.4
	k8s.io/apiserver v0.29.4
	k8s.io/client-go v0.29.4
	k8s.io/component-base v0.29.4
	k8s.io/klog v1.0.0
	k8s.io/kubelet v0.29.4
	// This needs to be pulled in because we are using
	// github.com/argoproj/gitops-engine
	k8s.io/kubernetes v1.29.7
	sigs.k8s.io/application v0.8.3
)

require (
	github.com/KimMachineGun/automemlimit v0.3.0
	github.com/argoproj/gitops-engine v0.7.0
	github.com/davecgh/go-spew v1.1.1
	github.com/deckarep/golang-set v1.7.1
	github.com/evanphx/json-patch v5.6.0+incompatible
	github.com/go-logr/glogr v1.2.2
	github.com/google/go-cmp v0.6.0
	github.com/google/go-github/v42 v42.0.0
	github.com/openshift/client-go v0.0.0-20231121143148-910ca30a1a9a
	github.com/prometheus/client_model v0.4.0
	github.com/prometheus/common v0.44.0
	golang.org/x/oauth2 v0.10.0
	gopkg.in/yaml.v3 v3.0.1 // indirect
	sigs.k8s.io/controller-runtime v0.15.1
	sigs.k8s.io/yaml v1.3.0
)

replace (
	k8s.io/api => k8s.io/api v0.29.4
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.29.4
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.29.4
	k8s.io/code-generator => k8s.io/code-generator v0.29.4
	k8s.io/component-helpers => k8s.io/component-helpers v0.29.4
	k8s.io/controller-manager => k8s.io/controller-manager v0.29.4
	k8s.io/cri-api => k8s.io/cri-api v0.29.4
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.29.4
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.29.4
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.29.4
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.29.4
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.29.4
	k8s.io/kubectl => k8s.io/kubectl v0.29.4
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.29.4
	k8s.io/metrics => k8s.io/metrics v0.29.4
	k8s.io/mount-utils => k8s.io/mount-utils v0.29.4
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.29.4
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.29.4
)
