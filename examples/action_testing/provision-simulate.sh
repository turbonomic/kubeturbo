sudo -E  "./actionexecutionsimulator" \
	--v=4 \
	--master="http://127.0.0.1:8080" \
	--etcd-servers="http://127.0.0.1:4001" \
	--pod="frontend-57ytg" \
	--action="provision" \
	--replica="3" > /tmp/kubeactionsimulator.log 2>&1