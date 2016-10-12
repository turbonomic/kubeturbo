## Deploy Kubeturbo on Existing Kubernetes Cluster

This guide is about how to run kubeturbo service as a *process* in the **Master-node** in an existing Kubernetes cluster running in a private datacenter. It also applies to a local Kubernetes cluster.

### Prerequisites
This example requires a running Kubernetes cluster. First check the current cluster status with kubectl.

```console
$ kubectl cluster-info
```

NOTE: this tutorial assumes there is no authentication for kube-apiserver. If there is authentication configured, please referred to the guide for [deploying Kubeturbo on AWS](https://github.com/vmturbo/kubeturbo/blob/master/deploy/kubeturbo-for-aws/README.md#step-one-create-kubeconfig) to create a valid kubeconfig and put it under */etc/kubeturbo*.

### Step One: Create Kubeturbo config

A Kubeturbo config is required for Kubeturbo service to connect to Ops Manager server remotely. You need to specify correct **Turbonomic Server address**, **username** and **password**.
**NOTE**: Turbonomic server address is "**<IP address of your ops manger>:80**".

The created config should be placed under */etc/kubeturbo/*.

```json
{
	"serveraddress":	"<SERVER_ADDRESS>",
	"localaddress":		"http://127.0.0.1/",
	"opsmanagerusername": 	"<USER_NAME>",
	"opsmanagerpassword": 	"<PASSWORD>"
}
```
you can find an example [here](https://raw.githubusercontent.com/vmturbo/kubeturbo/master/deploy/config)

### Step Two: Get Kubeturbo Binary

Kubeturbo binary can be downloaded [here](https://github.com/vmturbo/kubeturbo/raw/master/deploy/run-as-process/kubeturbo).

### Step Three:  Run Kubeturbo

Use the following command to run kubeturbo. Make sure you specify correct **Kubernetes_API_Server_Address** and **ETCD_Servers**. Also config is placed at the right place.

```console
sudo -E "./kubeturbo" \
	 --v=2 \
	 --master="<Kubernetes_API_Server_Address>" \
	 --etcd-servers="<ETCD_Servers>" \
	 --config-path="/etc/kubeturbo/config" > "/tmp/kubeturbo.log" 2>&1 &
```

If you use **kubeconfig**, then use the following command to start kubeturbo.
```console
sudo -E "./kubeturbo" \
	 --v=2 \
   --kubeconfig="<KUBECONFIG_PATH>" \
	 --etcd-servers="<ETCD_Servers>" \
	 --config-path="/etc/kubeturbo/config" > "/tmp/kubeturbo.log" 2>&1 &
```

Once started, the log of kubeturbo is generated at */tmp/kubeturbo.log*.

```console
tail -f /tmp/kubeturbo.log
```
