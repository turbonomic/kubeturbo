## Deploy Kubeturbo on Existing Kubernetes Cluster

This guide is about how to deploy **Kubeturbo** service in **OpenShift**.

### Prerequisites
This example requires a running Kubernetes cluster. First check the current cluster status with kubectl.

```console
$ kubectl cluster-info
```

### Create cAdvisor DaemonSet

**cAdvisor** is required for working with Kubeturbo. However, the cAdvisor port is closed by default in OpenShift. So we need to deploy cAdvisor onto every node in the cluster.

#### Step One: Create ServiceAccount
A ServiceAccount is needed for cAdvisor pods to access the OpenShift cluster.

##### Define Turbo-user Service Account

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: turbo-user
  namespace: default
```

[Download example](turbo-user-service-account.yaml?raw=true)

Then you would see turbo-user when you list service accounts in OpenShift.

```console
$kubectl get sa --namespace=default
NAMESPACE          NAME                        SECRETS   AGE
default            builder                     2         62d
default            default                     2         62d
default            deployer                    2         62d
default            registry                    2         62d
default            router                      2         62d
default            turbo-user                  2         25s
```

#### Step Two: Edit Security Context Constraint
In OpenShift, security context constraints allow administrator to control permissions for pods. As cAdvisor pods need privileged permissions, you need to add turbo-user service account to proper security context constraints. Here turbo-user is added to *privileged* security context constraint.

```console
$oc edit scc privileged
```

Then add "system:serviceaccount:default:turbo-user" under users, as shown

```console
users:
- system:serviceaccount:openshift-infra:build-controller
- system:serviceaccount:management-infra:management-admin
- system:serviceaccount:management-infra:inspector-admin
- system:serviceaccount:default:router
- system:serviceaccount:default:registry
- system:serviceaccount:osproj1:turbo-user
- admin
- system
- root
```

#### Step Three: Define cAdvisor DaemonSet

```yaml
apiVersion: extensions/v1beta1
kind: DaemonSet
metadata:
  name: cadvisor
  namespace: default
  labels:
    name: cadvisor
spec:
  template:
    metadata:
      labels:
        name: cadvisor
    spec:
      containers:
      - name: cadvisor
        image: google/cadvisor:latest
        securityContext:
          privileged: true
        ports:
          - name: http
            containerPort: 8080
            hostPort: 9999
        volumeMounts:
          - name: rootfs
            mountPath: /rootfs
            readOnly: true
          - name: varrun
            mountPath: /var/run
            readOnly: false
          - name: varlibdocker
            mountPath: /var/lib/docker
            readOnly: true
          - name: sysfs
            mountPath: /sys
            readOnly: true
      serviceAccount: turbo-user
      volumes:
        - name: rootfs
          hostPath:
            path: /
        - name: varrun
          hostPath:
            path: /var/run
        - name: varlibdocker
          hostPath:
            path: /var/lib/docker
        - name: sysfs
          hostPath:
            path: /sys
```
[Download example](cadvisor-daemonsets.yaml?raw=true)

#### Step Four: Deploy cAdvisor DaemonSet

```console
$kubectl create -f cadvisor-daemonset.yaml
daemonset "cadvisor" created
$kubectl get ds
NAME       DESIRED   CURRENT   NODE-SELECTOR   AGE
cadvisor   3         3         <none>          4m
$kubectl get po
NAME                         READY     STATUS    RESTARTS   AGE
cadvisor-5iyt4               1/1       Running   0          4m
cadvisor-918d2               1/1       Running   0          4m
cadvisor-bii3n               1/1       Running   0          4m
```


### Deploy Kubeturbo Service
Now cAdvisor is up and running on every node, we are ready to deploy Kubeturbo Service.

#### Step One: Label Master Node
As Kubeturbo is suggested to run on the master node, we need to create label for the Master node. To label the master node, simply execute the following command

```console
$kubectl label nodes <MASTER_NODE_NAME> role=master
```

To see the labels on master node (*which is 10.10.174.81 in this example*),

```console
$kubectl get no --show-labels
NAME           STATUS    AGE       LABELS
10.10.174.81   Ready     62d       kubernetes.io/hostname=10.10.174.81,region=primary,role=master
10.10.174.82   Ready     62d       kubernetes.io/hostname=10.10.174.82,region=primary
10.10.174.83   Ready     62d       kubernetes.io/hostname=10.10.174.83,region=primary
```

#### Step Two: Get Kubeconfig
A kubeconfig with proper permission is required for Kubeturbo service to interact with Kube-apiserver. If you have successfully started up your OpenShift cluster, you will find admin.kubeconfig under /etc/origin/master. Copy this kubeconfig file to /etc/kubeturbo/.

#### Step Three: Create Kubeturbo config

A Kubeturbo config is required for Kubeturbo service to connect to Ops Manager server remotely. You need to specify correct Turbonomic Server address, username and password.

The config should be placed under /etc/kubeturbo/

```json
{
    "serveraddress":		"<SERVER_ADDRESS>",
    "targettype":		"Kubernetes",
    "nameoraddress":  		"k8s_vmt",
    "username":			"kubernetes_user",
    "password":			"fake_password",
    "targetidentifier": 	"my_k8s",
    "localaddress":		"http://127.0.0.1/",
    "websocketusername": 	"vmtRemoteMediation",
    "websocketpassword": 	"vmtRemoteMediation",
    "opsmanagerusername": 	"<USER_NAME>",
    "opsmanagerpassword": 	"<PASSWORD>"
}
```

#### Step Four: Create Kubeturbo Pod

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
    role: master
  containers:
  - name: kubeturbo
    image: vmturbo/kubeturbo:1.0
    command:
      - /bin/kubeturbo
    args:
      - --v=3
      - --kubeconfig=/etc/kubeturbo/admin.kubeconfig
      - --etcd-servers=http://127.0.0.1:2379
      - --config-path=/etc/kubeturbo/config
      - --flag-path=/etc/kubeturbo/flag
      - --cadvisor-port=9999
    volumeMounts:
    - name: vmt-config
      mountPath: /etc/kubeturbo
      readOnly: true
  - name: etcd
    image: gcr.io/google_containers/etcd:2.0.9
    command:
    - /usr/local/bin/etcd
    - -data-dir
    - /var/etcd/data
    - -listen-client-urls
    - http://127.0.0.1:2379,http://127.0.0.1:4001
    - -advertise-client-urls
    - http://127.0.0.1:2379,http://127.0.0.1:4001
    - -initial-cluster-token
    - etcd-standalone
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

[Download example](kubeturbo-openshift.yaml?raw=true)

##### Deploy Kubeturbo Pod

```console
$kubectl create -d kubeturbo-openshift.yaml
pod "kubeturbo" created
$kubectl get pods --all-namespaces
NAME                         READY     STATUS    RESTARTS   AGE
cadvisor-5iyt4               1/1       Running   0          5m
cadvisor-918d2               1/1       Running   0          5m
cadvisor-bii3n               1/1       Running   0          5m
kubeturbo                    2/2       Running   0          54s
```

### Deploy K8sconntrack

With previous steps, Kubeturbo service is running and starting to collect resource comsuption metrics from each node, pod and application. Those metrics are continuously sent back to Turbonomic server. If you want Kubeturbo to collect network related metrics, such as service transaction counts and network flow information between pods inside current Kubernetes cluster, you need to deploy K8sconntrack monitoring service.

K8sconntrack monitoring service should be running on each node inside cluster. A detailed guide about how to deploy K8sconnection onto an OpenShift cluster can be found [here](https://github.com/DongyiYang/k8sconnection/blob/master/deploy/openshift_deploy/README.md).
