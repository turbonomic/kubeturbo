sudo -E "./kubeturbo" \
	 --v=3 \
	 --master="http://127.0.0.1:8080" \
	 --etcd-servers="http://127.0.0.1:4001" \
	 --config-path="./examples/vmt_testing_config.json" > "/tmp/kube-vmturbo.log" 2>&1 &
