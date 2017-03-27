OUTPUT_DIR=${OUTPUT_DIR:-"_output"}

./${OUTPUT_DIR}/kubeturbo \
	 --v=3 \
	 --master="http://172.17.0.1:8080" \
	 --config-path="$GOPATH/src/github.com/turbonomic/kubeturbo/cmd/kubeturbo/container-conf.json" > "/tmp/kubeturbo.log" 2>&1 &
