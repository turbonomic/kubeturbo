## Deploy Kubeturbo on AWS Kubernetes Cluster

Once deployed, the Kubeturbo service enables you to give Turbonomic visibility into a Kubernetes cluster running in AWS. Kubeturbo will be deployed as a mirror pod on Master nodes.

### Prerequisites
* Turbonomic 5.9+
* Running Kubernetes 1.4+ cluster 
> NOTE: to check the current status of your cluster, run the following command in the console:
> ```console
>$ kubectl cluster-info


### Step One: Creating the Kubeturbo Configuration Files

In order to connect to your Turbonomic installation, a Kubeturbo configuration file must be created.

Create a file called `config` in the `/etc/kubeturbo/` directory, with the following contents:

```json
{
	"communicationConfig": {
		"serverMeta": {
			"turboServer": "<TURBONOMIC_SERVER_IP_ADDRESS>"
		},
		"restAPIConfig": {
			"opsManagerUserName": "<TURBONOMIC_USERNAME>",
			"opsManagerPassword": "<TURBONOMIC_PASSWORD>"
		}
	},
	"targetConfig": {
		"probeCategory":"CloudNative",
		"targetType":"Kubernetes",
		"address":"<KUBERNETES_MASTER_ADDRESS>",
		"username":"<KUBERNETES_USERNAME>",
		"password":"<KUBERNETES_PASSWORD>"
	}
}
```
you can find an example with values [here](../config).

### Step Two: Creating the Kubeturbo Mirror Pod

> NOTE: Ensure that you have completed Step One.

Mirror pods are created by Kubelet. Based on whether kubeconfig is used, there are two ways to define the Kubeturbo pod template.

Copy the Kubeturbo yaml pod definition to the configuration path used by Kubelet master nodes on startup. Typically, `/etc/kubernetes/manifests/`.

#### Kubeturbo Pod Definition with kubeconfig

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: kubeturbo
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
      - --kubeconfig=<PATH_TO_KUBECONFIG>
      - --turboconfig=/etc/kubeturbo/config
    volumeMounts:
    - name: turbo-config
      mountPath: /etc/kubeturbo
      readOnly: true
    - name: kubeconfig-dir
      mountPath: <DIRECTORY_CONTAINS_KUBECONFIG>
      readOnly: true
  volumes:
  - name: turbo-config
    hostPath:
      path: /etc/kubeturbo
  - name: kubeconfig-dir
    hostPath:
      path: <DIRECTORY_CONTAINS_KUBECONFIG>
  restartPolicy: Always
```

[Download example](kubeturbo-with-kubeconfig.yaml?raw=true)

#### Kubeturbo Pod Definition without kubeconfig

If kube-apiserver running in master node and running on http://127.0.0.1:8080, a kubeturbo pod can be defined to directly access api-server without kubeconfig.
>As kubeturbo accesses api-server running on localhost, hostNetwork must set to true.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: kubeturbo
  labels:
    name: kubeturbo
spec:
  hostNetwork: true
  containers:
  - name: kubeturbo
    image: vmturbo/kubeturbo:latest
    command:
      - /bin/kubeturbo
    args:
      - --v=2
      - --master=http://127.0.0.1:8080
      - --turboconfig=/etc/kubeturbo/config
    volumeMounts:
    - name: turbo-config
      mountPath: /etc/kubeturbo
      readOnly: true
  volumes:
  - name: turbo-config
    hostPath:
      path: /etc/kubeturbo
  restartPolicy: Always
```

[Download example](kubeturbo-without-kubeconfig.yaml?raw=true)

The Kubeturbo mirror pod will be visible after several seconds. To verify that the Kubeturbo pod is running, use `kubectl get pods --all-namespaces` and look for "kubeturbo".

```console
NAMESPACE     NAME                                    READY     STATUS        RESTARTS   AGE
kube-system   kube-apiserver-10.10.174.116            1/1       Running       0          55s
kube-system   kubeturbo-10.10.174.116                 1/1       Running       0          55s
kube-system   kube-controller-manager-10.10.174.116   1/1       Running       0          55s
kube-system   kube-proxy-10.10.174.116                1/1       Running       0          55s
kube-system   kube-proxy-10.10.174.117                1/1       Running       0          10s
kube-system   kube-proxy-10.10.174.118                1/1       Running       0          10s
kube-system   kube-proxy-10.10.174.119                1/1       Running       0          10s
```
### Optional- Enable Action Execution

By default, Turbonomic will recommend the following actions for ContainerPods:
* Horizontal Scale Up
* Horizontal Scale Down
* Provision additional resources (VMem, VCPU)
* Move Pod across Virtual Machines

In order to make these actions executable from within Turbonomic, you must stop the default scheduler by removing kube-scheduler from both `/etc/kubernetes/manifests` and the source path you specified for the pod-master pod (Typically `/srv/kubernetes/manifests`).

### Optional- Enable Network Metric Collection via K8sconntrack

In order for Kubeturbo to collect network related metrics such as service transaction counts and network flow information between pods in the Kubernetes cluster, you must deploy the K8sconntrack monitoring service.

K8sconntrack should be running on each node in the cluster. A guide detailing how to deploy K8sconnection in a Kubernetes cluster can be found [here](https://github.com/DongyiYang/k8sconnection/blob/master/deploy/general_deploy/README.md).
