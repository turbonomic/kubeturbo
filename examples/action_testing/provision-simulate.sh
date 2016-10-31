sudo -E  "/home/dongyiyang/Sandbox/Go/src/github.com/vmturbo/kubeturbo/_output/kube-vmtactionsimulator" \
	--v=4 \
	--master="http://172.17.0.1:8080" \
	--etcd-servers="http://127.0.0.1:2379" \
	--pod="simple-server-hn538" \
	--action="provision" \
	--replica="3" > /tmp/kubeactionsimulator.log 2>&1
