OUTPUT_DIR=./_output

product: clean
	env GOOS=linux GOARCH=amd64 go build -o ${OUTPUT_DIR}/kubeturbo.linux ./cmd/kubeturbo

build: clean
	go build -o ${OUTPUT_DIR}/kubeturbo ./cmd/kubeturbo

test: clean
	@go test -v -race ./pkg/...
	
.PHONY: clean
clean:
	@: if [ -f ${OUTPUT_DIR} ] then rm -rf ${OUTPUT_DIR} fi

.PHONY: default test build
default: clean build test

.DEFAULT_GOAL := default
