### Prerequisites
* Turbonomic 5.9+
* Running Kubernetes 1.4+ cluster 

### <a name="configFile"></a>Step One: Copy kubeconfig for kubeturbo
`kubeconfig` with proper permission is required for Kubeturbo service to interact with kube-apiserver. 
Please copy `kubeconfig` to `/etc/kubeturbo` on the node Kubeturbo will be running on. You can label the node as following to make sure Kubeturbo will be deployed on that node.

```console
$kubectl label nodes <NODE_NAME> kubeturbo=deployable
```

To see the labels on node (*which is 10.10.174.81 in this example*),

```console
$kubectl get nodes --show-labels
NAME           STATUS    AGE       LABELS
10.10.174.81   Ready     62d       kubernetes.io/hostname=10.10.174.81,region=primary,kubeturbo=deployable
10.10.174.82   Ready     62d       kubernetes.io/hostname=10.10.174.82,region=primary
10.10.174.83   Ready     62d       kubernetes.io/hostname=10.10.174.83,region=primary
```


### Step Two: Creating Kubeturbo Pod
In order to connect to your Turbonomic installation, a Kubeturbo configuration file must be created. 

Create a file called `config` in the `/etc/kubeturbo/` directory in the same node labeled in previous step, with the following contents:

> The `<TURBONOMIC_SERVER_URL>` is typically `https://<TURBO_SERVER_IP>:443`
> The `<TURBONOMIC_SERVER_VERSION>` is Turbonomic release version, e.g. `5.9` or `6.0`

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

### Step Three: Creating Kubeturbo Pod

Assume you have `kubeconfig` and `config` under `/etc/kubeturbo`.
Define and run Kubeturbo pod using the following yaml.

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
    image: vmturbo/kubeturbo:6.0
    args:
      - --kubeconfig=/etc/kubeturbo/kubeconfig
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
