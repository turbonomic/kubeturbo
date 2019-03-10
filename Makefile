OUTPUT_DIR=./

SOURCE_DIRS = cmd pkg
PACKAGES := go list ./... | grep -v /vendor | grep -v /out


product: clean
	env GOOS=linux GOARCH=amd64 go build -o ${OUTPUT_DIR}/kubeturbo.linux ./cmd/kubeturbo

build: clean
	go build -o ${OUTPUT_DIR}/kubeturbo ./cmd/kubeturbo

docker: clean
	docker build -t vmturbo/kubeturbo:6.3 --build-arg GIT_COMMIT=$(shell git rev-parse --short HEAD) .

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
