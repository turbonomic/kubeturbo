OUTPUT_DIR=./

SOURCE_DIRS = cmd pkg
PACKAGES := go list ./... | grep -v /vendor | grep -v /out


product: clean
	env GOOS=linux GOARCH=amd64 go build -o ${OUTPUT_DIR}/kubeturbo.linux ./cmd/kubeturbo

debug-product: clean
	env GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -gcflags "-N -l" -o ${OUTPUT_DIR}/kubeturbo.debug ./cmd/kubeturbo

build: clean
	go build -o ${OUTPUT_DIR}/kubeturbo ./cmd/kubeturbo

docker: clean
	docker build -t turbonomic/kubeturbo:6.4dev --build-arg GIT_COMMIT=$(shell git rev-parse --short HEAD) .

delve: Dockerfile.delve
	docker build -f Dockerfile.delve -t delve:staging .
	docker create --name delve-staging delve:staging
	docker cp delve-staging:/root/bin/dlv .
	touch dlv
	docker rm delve-staging

debug: debug-product delve
	docker build -f Dockerfile.debug -t turbonomic/kubeturbo:6.4debug .

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
