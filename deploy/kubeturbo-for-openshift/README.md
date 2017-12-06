## Deploy Kubeturbo on Existing OpenShift Cluster

This guide is about how to deploy **Kubeturbo** service in **OpenShift**.

### Step One: Label Master Node
As Kubeturbo is suggested to run on the master node, we need to create label for the Master node. To label the master node, simply execute the following command

```console
$oc label nodes <MASTER_NODE_NAME> role=kubeturbo
```

To see the labels on master node (*which is 10.10.174.81 in this example*),

```console
$oc get no --show-labels
NAME           STATUS    AGE       LABELS
10.10.174.81   Ready     62d       kubernetes.io/hostname=10.10.174.81,region=primary,role=kubeturbo
10.10.174.82   Ready     62d       kubernetes.io/hostname=10.10.174.82,region=primary
10.10.174.83   Ready     62d       kubernetes.io/hostname=10.10.174.83,region=primary
```

### Step Two: Get Kubeconfig
A kubeconfig with proper permission is required for Kubeturbo service to interact with kube-apiserver. If you have successfully started up your OpenShift cluster, you will find admin.kubeconfig under /etc/origin/master. Copy this kubeconfig file to /etc/kubeturbo/.

### Step Three: Create Kubeturbo config

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

`UPDATE`: Starting from version 6.1.0, there is no need to specify `targetConfig` so that the `config` file looks like:

```json
{
	"communicationConfig": {
		"serverMeta": {
		    "turboServer": "<<SERVER_ADDRESS>>"
		},
		"restAPIConfig": {
		    "opsManagerUserName": "<USERNAME>",
		    "opsManagerPassword": "<PASSWORD>"
		}
	}
}
```

### Step Four: Create Kubeturbo Pod

Make sure you have **admin.kubeconfig** and **config** under */etc/kubeturbo*.

##### Define Kubeturbo pod

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: kubeturbo
  labels:
    name: kubeturbo
spec:
  nodeSelector:
    role: kubeturbo
  containers:
  - name: kubeturbo
    image: vmturbo/kubeturbo:os35
    args:
      - --kubeconfig=/etc/kubeturbo/admin.kubeconfig
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

[Download example](kubeturbo-openshift.yaml?raw=true)

##### Deploy Kubeturbo Pod

```console
$oc create -f kubeturbo-openshift.yaml
pod "kubeturbo" created

$oc get pods --all-namespaces
NAME                         READY     STATUS    RESTARTS   AGE
kubeturbo                    1/1       Running   0          54s
```

### Deploy K8sconntrack

By following previous steps, Kubeturbo service should be running and starting to collect resource consumption metrics from each node, pod and application. Those metrics are continuously sent back to Turbonomic server. If you want Kubeturbo to collect network related metrics, such as service transaction counts and network flow information between pods inside current Kubernetes cluster, you need to deploy K8sConntrack monitoring service.

K8sConntrack monitoring service should be running on each node inside cluster. A detailed guide about how to deploy K8sConntrack onto an OpenShift cluster can be found [here](https://github.com/DongyiYang/k8sconnection/blob/master/deploy/openshift_deploy/README.md).
