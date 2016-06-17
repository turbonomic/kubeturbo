sudo -E  "./actionexecutionsimulator" \
	--v=4 \
	--master="http://127.0.0.1:8080" \
	--etcd-servers="http://127.0.0.1:4001" \
	--vapp="vApp-apache2" \
	--application="apache2::default/frontend-qe6sk" \
	--action="unbind" > /tmp/kubeactionsimulator.log 2>&1