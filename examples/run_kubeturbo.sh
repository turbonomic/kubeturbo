sudo -E "./kubeturbo" \
	 --v=3 \
	 --master="http://127.0.0.1:8080" \
	 --etcd-servers="http://127.0.0.1:4001" \
	 --flag-path="$GOPATH/src/github.com/vmturbo/kubeturbo/examples/vmt_testing_flag.json" \
	 --config-path="$GOPATH/src/github.com/vmturbo/kubeturbo/examples/vmt_testing_config.json" > "/tmp/kubeturbo.log" 2>&1 &
