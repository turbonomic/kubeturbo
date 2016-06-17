sudo -E  "./actionexecutionsimulator" \
	 --v=4 \
	 --master="http://127.0.0.1:8080" \
	 --etcd-servers="http://127.0.0.1:4001" \
	 --pod="nginx2" \
	 --action="move" \
	 --destination="127.0.0.1">/tmp/kubeactionsimulator.log 2>&1