## Deploy Kubeturbo on Existing Kubernetes Cluster

This guide is about how to deploy kubeturbo service in an existing Kubernetes cluster running in a private datacenter. It also applies to a local Kubernetes cluster. Kubeturbo is deployed as mirror on Master nodes.

### Prerequisites
This example requires a running Kubernetes cluster. First check the current cluster status with kubectl.

```console
$ kubectl cluster-info
```

NOTE: this tutorial assumes there is no authentication for kube-apiserver. If there is authentication configured, please referred to the guide for deploying Kubeturbo on AWS.

### Step One: Create Kubeturbo config

A Kubeturbo config is required for Kubeturbo service to connect to Ops Manager server remotely. You need to specify correct **Turbonomic Server address**, **username** and **password**.
**NOTE**: Turbonomic server address is "**IP address of your ops manger**".

Create a file called **"config"** and put it under */etc/kubeturbo/*.

```json
{
	"serveraddress":	"<SERVER_ADDRESS>",
	"localaddress":		"http://127.0.0.1/",
	"opsmanagerusername": 	"<USER_NAME>",
	"opsmanagerpassword": 	"<PASSWORD>"
}
```
you can find an example [here](https://raw.githubusercontent.com/vmturbo/kubeturbo/master/deploy/config)


### Step Two: Create Kubeturbo Mirror Pod

Make sure you have **config** under */etc/kubeturbo* and you specify the correct **Kubernetes_API_Server_Address** and **ETCD_Servers**.

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
    image: vmturbo/kubeturbo:latest
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

K8sconntrack monitoring service should be running on each node inside cluster. A detailed guide about how to deploy K8sconnection onto a Kuberentes cluster can be found [here](https://github.com/DongyiYang/k8sconnection/blob/master/deploy/general_deploy/README.md).
