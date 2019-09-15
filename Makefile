OUTPUT_DIR=build
SOURCE_DIRS = cmd pkg
PACKAGES := go list ./... | grep -v /vendor | grep -v /out
SHELL := '/bin/bash'

product: clean
	env GOOS=linux GOARCH=amd64 go build -o ${OUTPUT_DIR}/kubeturbo ./cmd/kubeturbo

debug-product: clean
	env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -gcflags "-N -l" -o ${OUTPUT_DIR}/kubeturbo.debug ./cmd/kubeturbo

build: clean
	go build -o ${OUTPUT_DIR}/kubeturbo ./cmd/kubeturbo

docker: product
	cd build; docker build -t turbonomic/kubeturbo --build-arg GIT_COMMIT=$(shell git rev-parse --short HEAD) .

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

test: clean
	@go test -v -race ./pkg/...

.PHONY: clean
clean:
	@: if [ -f ${OUTPUT_DIR} ] then rm -rf ${OUTPUT_DIR} fi

.PHONY: fmtcheck
fmtcheck:
	@gofmt -l $(SOURCE_DIRS) | grep ".*\.go"; if [ "$$?" = "0" ]; then exit 1; fi

.PHONY: vet
vet:
	@go vet $(shell $(PACKAGES))
