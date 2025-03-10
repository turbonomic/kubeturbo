OUTPUT_DIR=build
SOURCE_DIRS=cmd pkg
PACKAGES=go list ./... | grep -v /vendor | grep -v /out
SHELL='/bin/bash'
REMOTE=github.ibm.com
USER=turbonomic
PROJECT=kubeturbo
BINARY=kubeturbo
DEFAULT_VERSION=latest
REMOTE_URL=$(shell git config --get remote.origin.url)
BRANCH=$(shell git rev-parse --abbrev-ref HEAD)
KUBETURBO_VERSION=8.15.4-SNAPSHOT
REVISION=$(shell git show -s --format=%cd --date=format:'%Y%m%d%H%M%S000')

YAMLLINT_VERSION := 1.32
KUBETURBO_OPERATOR_CRD_OPERATOR_HUB := deploy/kubeturbo-operator/deploy/crds/charts_v1alpha1_kubeturbo_crd.yaml
KUBETURBO_OPERATOR_CRD := deploy/kubeturbo-operator/config/crd/bases/charts.helm.k8s.io_kubeturboes.yaml


GIT_COMMIT=$(shell git rev-parse HEAD)
BUILD_TIME=$(shell date -R)
BUILD_TIMESTAMP=$(shell date +'%Y%m%d%H%M%S000')
PROJECT_PATH=$(REMOTE)/$(USER)/$(PROJECT)
VERSION=$(or $(KUBETURBO_VERSION), $(DEFAULT_VERSION))
LDFLAGS='\
 -X "$(PROJECT_PATH)/version.GitCommit=$(GIT_COMMIT)" \
 -X "$(PROJECT_PATH)/version.BuildTime=$(BUILD_TIME)" \
 -X "$(PROJECT_PATH)/version.Version=$(VERSION)"'

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.25.0

ENVTEST ?= $(LOCALBIN)/setup-envtest

LINUX_ARCH=amd64 arm64 ppc64le s390x

$(LINUX_ARCH): clean
	env GOOS=linux GOARCH=$@ go build -ldflags $(LDFLAGS) -o $(OUTPUT_DIR)/linux/$@/$(BINARY) ./cmd/kubeturbo

product: $(LINUX_ARCH)

debug-product: clean
	env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags $(LDFLAGS) -gcflags "-N -l" -o $(OUTPUT_DIR)/$(BINARY).debug ./cmd/kubeturbo

build: clean
	go build -ldflags $(LDFLAGS) -o $(OUTPUT_DIR)/$(BINARY) ./cmd/kubeturbo

buildInfo:
		$(shell test -f git.properties && rm -rf git.properties)
		@echo 'turbo-version.remote.origin.url=$(REMOTE_URL)' >> git.properties
		@echo 'turbo-version.commit.id=$(GIT_COMMIT)' >> git.properties
		@echo 'turbo-version.branch=$(BRANCH)' >> git.properties
		@echo 'turbo-version.branch.version=$(VERSION)' >> git.properties
		@echo 'turbo-version.commit.time=$(REVISION)' >> git.properties
		@echo 'turbo-version.build.time=$(BUILD_TIMESTAMP)' >> git.properties

integration: clean
	go test -c -o $(OUTPUT_DIR)/integration.test ./test/integration

docker: product
	cd build; DOCKER_BUILDKIT=1 docker build -t turbonomic/kubeturbo .

delve:
	docker build -f build/Dockerfile.delve -t delve:staging .
	docker create --name delve-staging delve:staging
	docker cp delve-staging:/root/bin/dlv ${OUTPUT_DIR}/
	touch dlv
	docker rm delve-staging

debug: debug-product delve
	@if [ ! -z ${TURBO_REPO} ] && [ ! -z ${KUBE_VER} ];	then \
		cd build; docker build -f Dockerfile.debug -t ${TURBO_REPO}/kubeturbo:${KUBE_VER}debug . ; \
	else \
		echo "Either dockerhub repo or kuberturbo version is not defined: TURBO_REPO=${TURBO_REPO} - KUBE_VER=${KUBE_VER}"; \
		echo "Please define both TURBO_REPO='dockerhub repository' and KUBE_VER='kubeturbo version'"; \
	fi

test: clean envtest
	@KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test -v -race ./pkg/...

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest


.PHONY: clean
clean:
	@if [ -f ${OUTPUT_DIR} ]; then rm -rf ${OUTPUT_DIR}/linux; fi

.PHONY: fmtcheck
fmtcheck:
	@gofmt -l $(SOURCE_DIRS) | grep ".*\.go"; if [ "$$?" = "0" ]; then exit 1; fi

.PHONY: vet
vet:
	@go vet $(shell $(PACKAGES))

.PHONY: python
PYTHON = $(LOCALBIN)/python3
python: $(PYTHON)  ## Install Python locally if necessary. Darwin OS is specific to mac users if running locally
$(PYTHON):
	@if ! command -v python3 >/dev/null 2>&1; then \
		mkdir -p $(LOCALBIN); \
		if [ `uname -s` = "Darwin" ]; then \
			brew install python@3; \
		else \
			sudo apt update && sudo apt install python3; \
		fi \
	fi
	mkdir -p $(dir $(PYTHON))
	ln -sf `command -v python3` $(PYTHON)

.PHONY: yamllint
yamllint: python
	@command -v $(LOCALBIN)/$(PYTHON)/yamllint >/dev/null 2>&1 || (echo "Installing yamllint $(YAMLLINT_VERSION)" && $(PYTHON) -m pip install yamllint==$(YAMLLINT_VERSION))

yaml-lint-check: yamllint
	$(PYTHON) -m yamllint -d '{extends: default, rules: {line-length: {max: 180, level: warning}, indentation: {indent-sequences: whatever}}}' $(KUBETURBO_OPERATOR_CRD_OPERATOR_HUB) $(KUBETURBO_OPERATOR_CRD)
	rm -rf ./bin

PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
REPO_NAME ?= icr.io/cpopen/turbonomic
.PHONY: multi-archs
multi-archs:
	env GOOS=${TARGETOS} GOARCH=${TARGETARCH} CGO_ENABLED=0 go build -ldflags $(LDFLAGS) -o $(OUTPUT_DIR)/$(BINARY) ./cmd/kubeturbo
.PHONY: docker-buildx
docker-buildx:
	docker buildx create --name kubeturbo-builder
	- docker buildx use kubeturbo-builder
	- docker buildx build --platform=$(PLATFORMS) --label "git-commit=$(GIT_COMMIT)" --label "git-version=$(VERSION)" --provenance=false --push --tag $(REPO_NAME)/kubeturbo:$(VERSION) -f build/Dockerfile.multi-archs --build-arg VERSION=$(VERSION) .
	docker buildx rm kubeturbo-builder

check-upstream-dependencies:
	./hack/check_upstream_dependencies.sh

# Check, Lint, and Test:
#   - Runs code checks including formatting, linting, and vetting.
#   - Runs tests to verify code functionality and correctness.
.PHONY: check-lint-test
check-lint-test:
	@echo "Checking, linting, and testing..."
	make fmtcheck
	make vet
	make yaml-lint-check
	make test
	@echo "Check, lint, and test completed."
