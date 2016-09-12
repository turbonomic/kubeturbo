## Deploy Kubeturbo on Existing Kubernetes Cluster

This guide is about how to deploy kubeturbo service in an existing Kubernetes cluster running in a private datacenter. It also applies to a local Kubernetes cluster. Kubeturbo is deployed as mirror on Master nodes.

### Prerequisites
This example requires a running Kubernetes cluster. First check the current cluster status with kubectl.

```console
$ kubectl cluster-info
```

NOTE: this tutorial assumes there is no authentication for kube-apiserver. If there is authentication configured, please referred to the guide for deploying Kubeturbo on AWS.

### Step One: Create Kubeturbo config

A Kubeturbo config is required for Kubeturbo service to connect to Ops Manager server remotely. You need to specify correct Turbonomic Server address, username and password.

The created config should be placed under /etc/kubeturbo/

```json
{
	"serveraddress":	"<SERVER_ADDRESS>",
	"targettype":		"Kubernetes",
	"nameoraddress":  	"k8s_vmt",
	"username":		"kubernetes_user",
	"targetidentifier": 	"my_k8s",
	"password":		"fake_password",
	"localaddress":		"http://127.0.0.1/",
	"websocketusername": 	"vmtRemoteMediation",
	"websocketpassword": 	"vmtRemoteMediation",
	"opsmanagerusername": 	"<USER_NAME>",
	"opsmanagerpassword": 	"<PASSWORD>"
}
```

### Step Two: Create Kubeturbo Mirror Pod

Here you need to get the IP address and port number for the kube-apiserver.

#### Define Kubeturbo pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: kubeturbo
  namespace: kube-system
  labels:
    name: kubeturbo
spec:
  containers:
  - name: kubeturbo
    image: vmturbo/kubeturbo:1.0
    command:
      - /bin/kubeturbo
    args:
      - --v=2
      - --master=<Kubernetes_API_Server_Address>
      - --etcd-servers=http://127.0.0.1:2379
      - --config-path=/etc/kubeturbo/config
    volumeMounts:
    - name: vmt-config
      mountPath: /etc/kubeturbo
      readOnly: true
  - name: etcd
    image: gcr.io/google_containers/etcd:2.0.9
    resources:
      limits:
        cpu: 100m
        memory: 50Mi
    command:
    - /usr/local/bin/etcd
    - -data-dir
    - /var/etcd/data
    - -listen-client-urls
    - http://127.0.0.1:2379,http://127.0.0.1:4001
    - -advertise-client-urls
    - http://127.0.0.1:2379,http://127.0.0.1:4001
    - -initial-cluster-token
    - etcd-kubeturbo
    volumeMounts:
    - name: etcd-storage
      mountPath: /var/etcd/data
  volumes:
  - name: etcd-storage
    emptyDir: {}
  - name: vmt-config
    hostPath:
      path: /etc/kubeturbo
  restartPolicy: Always

```

[Download example](kubeturbo.yaml?raw=true)

#### Create Kubeturbo pod

As mirror pods are created by Kubelet, you can simply copy kubeturbo yaml definition to your config path you specified when you starts kubelet on master nodes. Usually, the path is /etc/kubernetes/manifests/.
At the same time, you need to stop the default scheduler by removing kube-scheduler from both /etc/kubernetes/manifests and the source path you specified for pod-master pod, which is usually /srv/kubernetes/manifests by default.

After several seconds, you will be able to see Kubeturbo mirror pod is running.

```console
$kubectl get pods --all-namespaces
NAMESPACE     NAME                                    READY     STATUS        RESTARTS   AGE
kube-system   kube-apiserver-10.10.174.116            1/1       Running       0          55s
kube-system   kubeturbo-10.10.174.116                 1/1       Running       0          55s
kube-system   kube-controller-manager-10.10.174.116   1/1       Running       0          55s
kube-system   kube-proxy-10.10.174.116                1/1       Running       0          55s
kube-system   kube-proxy-10.10.174.117                1/1       Running       0          10s
kube-system   kube-proxy-10.10.174.118                1/1       Running       0          10s
kube-system   kube-proxy-10.10.174.119                1/1       Running       0          10s
```
### Deploy K8sconntrack

With previous steps, Kubeturbo service is running and starting to collect resource comsuption metrics from each node, pod and applications. Those metrics are continuously sent back to Turbonomic server. If you want Kubeturbo to collect network related metrics, such as service transaction counts and network flow information between pods inside current Kubernetes cluster, you need to deploy K8sconntrack monitoring service.

K8sconntrack monitoring service should be running on each node inside cluster. A detailed guide about how to deploy K8sconnection onto a Kuberentes cluster can be found [here](https://github.com/DongyiYang/k8sconnection/blob/master/examples/deploy_k8sconntrack/general_deploy/README.md).
