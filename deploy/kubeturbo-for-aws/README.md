## Deploy Kubeturbo on AWS Kubernetes Cluster

This guide is about how to deploy kubeturbo service in an existing Kubernetes cluster running on AWS. Kubeturbo is deployed as mirror on Master nodes. So whenever Kubeturbo stops running for any reason, Kubelet on master node would restart Kubeturbo pod automatically.

NOTE: Some of the procedure is outdated and will be updated shortly.  Please check back in a couple of days.

### Prerequisites
This example requires a running Kubernetes cluster. First check the current cluster status with kubectl.

```console
$ kubectl cluster-info
```

Also, make sure you have all the cluster authentication files ready, including the certificate authority file, admin certificate and admin key.

### Step One: Create Kubeconfig

Connect to your Kubernetes master via SSH.  Create a directory called kubeturbo in the /etc directory.  This is the directory where you will store your Kubeturbo configuration files.

```console
$ mkdir /etc/kubeturbo
```

#### Option 1: Copy the existing one

You can copy the existing kubeconfig that was generated when you deployed your Kubernetes cluster on AWS. The file should be named kubeconfig and be placed into the /etc/kubeturbo dirctory which was created in the previous step. Also, make sure you copy the whole credentials folder into the /etc/kubeturbo directory. In the credentials directory, make sure there is an admin-key.pem, an admin.pem and a ca.pem.

#### Option 2: Generate From Certificate

You can also generate a kubeconfig file from your certificates. The [create_kubeconfig.sh](https://raw.githubusercontent.com/vmturbo/kubeturbo/master/deploy/create_kubeconfig.sh) can help you generated it quickly with all the necessary certicates embeded.

In order to run create_kubeconfig.sh, you need to provide the api-server address and correct certicates. For example:

```console
$ ./create_kubeconfig --server=<SERVER_ADDRESS> --ca=<PATH_TO_YOUR_CA_FILE> --cert=<PATH_TO_YOUR_CERTIFICATE_FILE> --key=<PATH_TO_YOUR_KEY_FILE>
```
A new config file named kubeconfig will then be generated and placed under /etc/kubeturbo/.

### Step Two: Create Kubeturbo config

A Kubeturbo config is required for Kubeturbo service to connect to Ops Manager server remotely. You need to specify correct **Turbonomic Server address**, **username** and **password**.
**NOTE**: Turbonomic server address is "**IP address of your ops manager**".

Create a file called **"config"** and put it under */etc/kubeturbo/*.

```json
{
	"communicationConfig": {
		"serverMeta": {
			"turboServer": "<SERVER_ADDRESS>"
		},
		"restAPIConfig": {
			"opsManagerUserName": "<USERNAME>",
			"opsManagerPassword": "<PASSWORD>"
		}
	},
	"targetConfig": {
		"probeCategory":"CloudNative",
		"targetType":"OpenShift",
		"address":"<OPENSHIFT_MASTER_ADDRESS>",
		"username":"<OPENSHIFT_USERNAME>",
		"password":"<OPENSHIFT_PASSWORD>"
	}
}
```
you can find an example [here](../config).

### Step Three: Create Kubeturbo Mirror Pod

#### Define Kubeturbo pod

Make sure you have **kubeconfig** and **config** under */etc/kubeturbo* and you specify the correct **ETCD_Servers**.

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
      - --kubeconfig=/etc/kubeturbo/kubeconfig
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

[Download example](kubeturbo-aws.yaml?raw=true)

#### Create Kubeturbo pod

As mirror pods are created by Kubelet, you can simply copy the kubeturbo yaml definition to your config path you specified when you start the kubelet on master nodes. Usually, the path is /etc/kubernetes/manifests/.
At the same time, you need to stop the default scheduler by removing kube-scheduler from both /etc/kubernetes/manifests and the source path you specified for pod-master pod, which is usually /srv/kubernetes/manifests by default. 

After several seconds, you will be able to see Kubeturbo mirror pod is running. (For some versions of kubernetes, you may not see mirror pod from the command below, please use docker ps to check if kubeturbo is running)

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

With previous steps, Kubeturbo service is running and starting to collect resource consuption metrics from each node, pod and applications. Those metrics are continuously sent back to the Turbonomic Autonomic Platform instance. If you want Kubeturbo to collect network related metrics, such as service transaction counts and network flow information between pods inside current Kubernetes cluster, you need to deploy K8sconntrack monitoring service.

K8sconntrack monitoring service should be running on each node inside cluster. A detailed guide about how to deploy K8sconnection onto a Kuberentes cluster running on AWS can be found [here](https://github.com/DongyiYang/k8sconnection/blob/master/deploy/aws_deploy/README.md). Since you already created a Kubeconfig, you can skip to step 2.
