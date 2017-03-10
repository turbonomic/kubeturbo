sudo -E  "/home/dongyiyang/Sandbox/Go/src/github.com/vmturbo/kubeturbo/_output/kube-vmtactionsimulator" \
	 --v=4 \
	 --master="http://172.17.0.1:8080" \
	 --pod="$1" \
	 --action="move" \
	 --destination="127.0.0.1">/tmp/kubeactionsimulator.log 2>&1
