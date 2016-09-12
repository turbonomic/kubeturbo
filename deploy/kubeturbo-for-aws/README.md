## Deploy Kubeturbo on AWS Kubernetes Cluster

This guide is about how to deploy kubeturbo service in an existing Kubernetes cluster running on AWS. Kubeturbo is deployed as mirror on Master nodes. So whenever Kubeturbo stops running for any reason, Kubelet on master node would restart Kubeturbo pod automatically.

### Prerequisites
This example requires a running Kubernetes cluster. First check the current cluster status with kubectl.

```console
$ kubectl cluster-info
```

Also, make sure you have all the cluster authentication files ready, including certificate authority file, admin certificate and admin key.

### Step One: Create Kubeconfig

#### Option 1: Copy the existing one

You can copy the existing kubeconfig generated when you deploy your Kubernetes cluster on AWS. So make sure you copy kubeconfig as well as the whole credentials folder into /etc/kubeturbo dir. In credentials dir, make sure there are admin-key.pem, admin.pem and ca.pem.

#### Option 2: Generate From Certificate

You can also generate a kubeconfig file from your certificates. The [create_kubeconfig.sh](https://raw.githubusercontent.com/vmturbo/kubeturbo/master/examples/deploy_kubeturbo/create_kubeconfig.sh) can help you quick generated it with all necessary certicates embeded.

In order to run create_kubeconfig.sh, you need to provide api-server address and correct certicates. For example:

```console
$ ./create_kubeconfig --server=<SERVER_ADDRESS> --ca=<PATH_TO_YOUR_CA_FILE> --cert=<PATH_TO_YOUR_CERTIFICATE_FILE> --key=<PATH_TO_YOUR_KEY_FILE>
```
Then a new config file named kubeconfig is generated and placed under /etc/kubeturbo/.

### Step Two: Create Kubeturbo config

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

### Step Three: Create Kubeturbo Mirror Pod

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
      - --kubeconfig=/etc/kubeturbo/kubeconfig
      - --etcd-servers=http://127.0.0.1:2379
      - --config-path=/etc/kubeturbo/config
      - --flag-path=/etc/kubeturbo/flag
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

[Download example](kubeturbo-aws.yaml?raw=true)

#### Create Kubeturbo pod

As mirror pods are created by Kubelet, you can simply copy kubeturbo yaml definition to your config path you specified when you starts kubelet on master nodes. Usually, the path is /etc/kubernetes/manifests/.
At the same time, you need to stop the default scheduler by removing kube-scheduler from both /etc/kubernetes/manifests and the source path you specified for pod-master pod, which is usually /srv/kubernetes/manifests by default. 

After several seconds, you will be able to see Kubeturbo mirror pod is running.

```console
$kubectl get pods --all-namespaces
NAMESPACE     NAME                                                READY     STATUS    RESTARTS   AGE
kube-system   kube-apiserver-ip-10-0-0-50.ec2.internal            1/1       Running   0          22d
kube-system   kube-proxy-ip-10-0-0-50.ec2.internal                1/1       Running   0          22d
kube-system   kube-proxy-ip-10-0-0-198.ec2.internal               1/1       Running   0          22d
kube-system   kube-controller-manager-ip-10-0-0-50.ec2.internal   1/1       Running   0          22d
kube-system   kubeturbo-ip-10-0-0-50.ec2.internal                 2/2       Running   1          1h
```
### Deploy K8sconntrack

With previous steps, Kubeturbo service is running and starting to collect resource comsuption metrics from each node, pod and applications. Those metrics are continuously sent back to Turbonomic server. If you want Kubeturbo to collect network related metrics, such as service transaction counts and network flow information between pods inside current Kubernetes cluster, you need to deploy K8sconntrack monitoring service.

K8sconntrack monitoring service should be running on each node inside cluster. A detailed guide about how to deploy K8sconnection onto a Kuberentes cluster running on AWS can be found [here](https://github.com/DongyiYang/k8sconnection/blob/master/deploy/aws_deploy/README.md).
