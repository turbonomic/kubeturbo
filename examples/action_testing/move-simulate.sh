sudo -E  "/home/dongyiyang/Sandbox/Go/src/github.com/vmturbo/kubeturbo/_output/kube-vmtactionsimulator" \
	 --v=4 \
	 --master="http://172.17.0.1:8080" \
	 --etcd-servers="http://127.0.0.1:2379" \
	 --pod="mynignx-1358535180-ezd9j" \
	 --action="move" \
	 --destination="127.0.0.1">/tmp/kubeactionsimulator.log 2>&1
