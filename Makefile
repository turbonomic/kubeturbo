OUTPUT_DIR=./_output

build: clean
	go build -o ${OUTPUT_DIR}/kubeturbo ./cmd/kubeturbo

	
.PHONY: clean
clean:
	@: if [ -f ${OUTPUT_DIR} ] then rm -rf ${OUTPUT_DIR} fi
