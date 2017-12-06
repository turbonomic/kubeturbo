### Prerequisites
* Turbonomic 6.0+
* Running Google Kubernetes cluster 1.6+ (Verified on 1.6, 1.7, and 1.8)

### <a name="configFile"></a>Step One: Create a secret for kubeturbo configuration file

Create a file called `config`, with the following contents:

> The `<TURBONOMIC_SERVER_URL>` is typically `https://<TURBO_SERVER_IP>:443`

> The `<TURBONOMIC_SERVER_VERSION>` is Turbonomic release version, e.g. `6.0.0`

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
	},
	"targetConfig": {
		"probeCategory":"CloudNative",
		"targetType":"Kubernetes",
		"address":"http://<K8S_MASTER_IP>:8080",
		"username":"user1",
		"password":"pass1"
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

Create a [`secret`](https://kubernetes.io/docs/concepts/configuration/secret/) with the configuration file:
```console
kubectl create secret generic vmt-config --from-file=path/to/config
```
Make sure the secret is created correctly:
```console
$ kubectl get secret
NAME        TYPE        DATA        AGE
vmt-config  Opaque      3       5s 
```


### Step Two: Create kubeturbo Pod

Create a yaml file `kubeturbo-gke.yaml` for kubeturbo Pod with the following content:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: kubeturbo-gke
  labels:
    name: kubeturbo
spec:
  containers:
  - name: kubeturbo-60
    image: vmturbo/kubeturbo:6.0 
    imagePullPolicy: Always
    args:
      - --turboconfig=/etc/kubeturbo/config
      - --v=3
    volumeMounts:
    - name: vmt-config
      mountPath: /etc/kubeturbo
      readOnly: true
  volumes:
  - name: vmt-config
    secret: 
      secretName: vmt-config  
  restartPolicy: Always
```
Note: the `--v=3` argument is to control the verbose level of log.

Create the Pod:
```console
kubectl create -f kubeturbo-gke.yaml
```

To verify that the Kubeturbo pod is running, use `kubectl get pods --all-namespaces` and look for "kubeturbo".

```console
NAMESPACE     NAME                                    READY     STATUS        RESTARTS   AGE
kube-system   kube-apiserver-10.10.174.116            1/1       Running       0          55s
default       kubeturbo                               1/1       Running       0          55s
kube-system   kube-controller-manager-10.10.174.116   1/1       Running       0          55s
kube-system   kube-proxy-10.10.174.116                1/1       Running       0          55s
kube-system   kube-proxy-10.10.174.117                1/1       Running       0          10s
kube-system   kube-proxy-10.10.174.118                1/1       Running       0          10s
kube-system   kube-proxy-10.10.174.119                1/1       Running       0          10s
```
