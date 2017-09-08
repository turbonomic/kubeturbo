### Prerequisites
* Turbonomic 5.9+
* Running Kubernetes 1.4+ cluster 

### <a name="configFile"></a>Step One: Creating the Kubeturbo Configuration Files

In order to connect to your Turbonomic installation, a Kubeturbo configuration file must be created. 

Create a file called `config` in the `/etc/kubeturbo/` directory, with the following contents:

> The `<TURBONOMIC_SERVER_URL>` is typically `https://<TURBO_SERVER_IP>:443`
> The `<TURBONOMIC_SERVER_VERSION>` is Turbonomic release version, e.g. `5.9.0` or `6.0.0`

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

### Step Two: Creating Kubeturbo Pod

> NOTE: Ensure that you have completed Step One.

Make sure you have `kubeconfig` and `config` under `/etc/kubeturbo`. Please copy them to `/etc/kubeturbo` on the node Kubeturbo will be running on. You can label the node as following to make sure Kubeturbo will be deployed on that node.

```console
$oc label nodes <NODE_NAME> kubeturbo=deployable
```
Define Kubeturbo pod

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
    image: vmturbo/kubeturbo:5.9
    command:
      - /bin/kubeturbo
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
