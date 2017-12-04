## Deploy Kubeturbo on Existing OpenShift Cluster

This guide is about how to deploy **Kubeturbo** service in **OpenShift**.

### Step One: Copy kubeconfig for kubeturbo
kubeconfig with proper permission is required for Kubeturbo service to interact with kube-apiserver. If you have successfully started up your OpenShift cluster, mostly you will find admin.kubeconfig under /etc/origin/master. Please copy this file to /etc/kubeturbo on the node Kubeturbo will be running on. You can label the node as following to make sure Kubeturbo will be deployed on that node.

```console
$oc label nodes <NODE_NAME> kubeturbo=deployable
```

To see the labels on node (*which is 10.10.174.81 in this example*),

```console
$oc get no --show-labels
NAME           STATUS    AGE       LABELS
10.10.174.81   Ready     62d       kubernetes.io/hostname=10.10.174.81,region=primary,kubeturbo=deployable
10.10.174.82   Ready     62d       kubernetes.io/hostname=10.10.174.82,region=primary
10.10.174.83   Ready     62d       kubernetes.io/hostname=10.10.174.83,region=primary
```

### Step Two: Create Kubeturbo config

A Kubeturbo config is required for Kubeturbo service to connect to Ops Manager server remotely. You need to specify correct **Turbonomic Server address**, **username** and **password**.
> The `<SERVER_IP_ADDRESS>` is "**IP address of your ops manager**".
> The `<TURBONOMIC_SERVER_VERSION>` is Turbonomic release version, e.g. `5.9.0` or `6.0.0`

Create a file called **"config"** and put it under */etc/kubeturbo/*.

```json
{
	"communicationConfig": {
		"serverMeta": {
                        "version": "<TURBONOMIC_SERVER_VERSION>",
			"turboServer": "https://<SERVER_IP_ADDRESS>:443"
		},
		"restAPIConfig": {
			"opsManagerUserName": "<USERNAME>",
			"opsManagerPassword": "<PASSWORD>"
		}
	},
	"targetConfig": {
		"probeCategory":"CloudNative",
		"targetType":"OpenShift",
		"address":"https://<OPENSHIFT_MASTER_ADDRESS>:8443",
		"username":"<OPENSHIFT_USERNAME>",
		"password":"<OPENSHIFT_PASSWORD>"
	}
}
```

`UPDATE`: Starting from version 6.1.0, there is no need to specify `targetConfig` so that the `config` file looks like:

```json
{
	"communicationConfig": {
		"serverMeta": {
                    "version": "<TURBONOMIC_SERVER_VERSION>",
		    "turboServer": "<TURBONOMIC_SERVER_URL>"
		},
		"restAPIConfig": {
			"opsManagerUserName": "<TURBONOMIC_USERNAME>",
			"opsManagerPassword": "<TURBONOMIC_PASSWORD>"
		}
	}
}
```

### Step Three: Create Kubeturbo Pod

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
    kubeturbo: deployable
  containers:
  - name: kubeturbo
    image: vmturbo/kubeturbo:60os
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
$oc create -f <kubeturbo_yaml>.yaml
pod "kubeturbo" created

$oc get pods --all-namespaces
NAME                         READY     STATUS    RESTARTS   AGE
kubeturbo                    1/1       Running   0          54s
```
