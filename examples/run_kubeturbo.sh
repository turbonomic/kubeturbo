OUTPUT_DIR=${OUTPUT_DIR:-"_output"}

sudo -E "./${OUTPUT_DIR}/kubeturbo" \
	 --v=4 \
	 --master="http://172.17.0.1:8080" \
	 --flag-path="$GOPATH/src/github.com/vmturbo/kubeturbo/examples/vmt_testing_flag.json" \
	 --config-path="$GOPATH/src/github.com/vmturbo/kubeturbo/cmd/kubeturbo/container-conf.json" > "/tmp/kubeturbo.log" 2>&1 &
