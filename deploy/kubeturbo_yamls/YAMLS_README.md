## Kubeturbo Deploy via YAMLs

Review the prerequisites defined in the [Deploy Overview](https://github.com/turbonomic/kubeturbo/tree/master/deploy/README.md)

This document describes the resources you will create to deploy kubeturbo, and values you would want to change for your deployment.  It is **strongly advised** you start with the sample yamls provided [here](https://github.com/turbonomic/kubeturbo/tree/master/deploy/kubeturbo_yamls).

#### Resources Overview
**1.** Create a namespace

Use an existing namespace, or create one where to deploy kubeturbo. The yaml examples all will use `turbo`.

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: turbo 
```

**2.** Create a service account, and add the role of cluster-admin
```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: turbo-user
  namespace: turbo
```

Assign `cluster-admin` role by cluster role binding:
```yaml
kind: ClusterRoleBinding
# For OpenShift 3.4-3.7 use apiVersion: v1
# For kubernetes 1.9 use rbac.authorization.k8s.io/v1
# For kubernetes 1.8 use rbac.authorization.k8s.io/v1beta1
apiVersion: rbac.authorization.k8s.io/v1beta1    
metadata:
  name: turbo-all-binding
  namespace: turbo
subjects:
- kind: ServiceAccount
  name: turbo-user
  namespace: turbo
roleRef:
  kind: ClusterRole
  name: cluster-admin
  # For OpenShift v3.4 remove apiGroup line
  apiGroup: rbac.authorization.k8s.io
```

**3.** create a configMap for kubeturbo, The <TURBONOMIC_SERVER_VERSION> is Turbonomic release version, e.g. 6.3.0 or 6.2.8.  To distinguish between different k8s clusters, supply a targetName value which will name the k8s cluster groups created in Turbonomic.
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: turbo-config
  namespace: turbo
data:
  turbo.config: |-
    {
        "communicationConfig": {
            "serverMeta": {
                "version": "<TURBONOMIC_SERVER_VERSION>",
                "turboServer": "https://<Turbo_server_URL>"
            },
            "restAPIConfig": {
                "opsManagerUserName": "<Turbo_username>",
                "opsManagerPassword": "<Turbo_password>"
            }
        },
        "targetConfig": {
            "targetName":"whateverYouWant"
        }
    }
```


**4.** Create a deployment for kubeturbo.  The image tag used will depend somewhat on your Turbo Server version.  For Server versions of 6.1.x - 6.2.x, use tag "6.2".  For Server versions of 6.3.1+, use "6.3".  Running CWOM? Go here to see conversion chart for [CWOM -> Turbonomic Server version](https://github.com/turbonomic/kubeturbo/tree/master/deploy/CWOM_versions.md).
```yaml
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: kubeturbo
  namespace: turbo
  labels:
    app: kubeturbo
spec:
  replicas: 1
  template:
    metadata:
      annotations:
        kubeturbo.io/controllable: "false"
      labels:
        app: kubeturbo
    spec:
      serviceAccount: turbo-user
      containers:
        - name: kubeturbo
          # Replace the image with desired version
          image: vmturbo/kubeturbo:6.3
          imagePullPolicy: IfNotPresent
          args:
            - --turboconfig=/etc/kubeturbo/turbo.config
            - --v=2
            # Comment out the following two args if running k8s 1.10 or older
            - --kubelet-https=true
            - --kubelet-port=10250
            # Uncomment the following arg if using IP for stitching
            #- --stitch-uuid=false
          volumeMounts:
          - name: turbo-volume
            mountPath: /etc/kubeturbo
            readOnly: true
          - name: varlog
            mountPath: /var/log
      volumes:
      - name: turbo-volume
        configMap:
          name: turbo-config
      - name: varlog
          emptyDir: {}
      restartPolicy: Always
```
Note: If Kubernetes version is older than 1.6, then add another arg for move/resize action `--k8sVersion=1.5`

#### Updating Turbo Server
When you update the Turbonomic or CWOM Server, you will need to update the configMap resource to reflect the new version.

1. After the update, obtain the new version.  To get this from the UI, go to Settings -> Updates -> About and use the numeric version such as “6.0.11” or “6.2.0” (Build details not required)
1. Edit the configMap resource in the k8s/OS dashboard, or by CLI using kubectl or oc “edit configMap” command. In the CLI example, substitute your configMap resource name (example “turbo-config”) and namespace/project (example “turbo”):
   `kubectl edit configMap turbo-config -n turbo`
1. Change the value of `"version":`
1. Delete the running kubeturbo pod to pick up new value
1. Repeat for every kubernetes / OpenShift cluster with a kubeturbo pod

#### Updating Kubeturbo Image
You may be instructed to update the kubeturbo pod image.  Determine which new tag you will be using.  You will either be instructed by Turbonomic to use a new image, or you may want to refresh the image to pick up a security patch.

1. Edit the deployment via the k8s/OS dashboard or using the CLI kubectl or oc “edit deployment” command. In the CLI example below, substitute your kubeturbo deployment name for (example “kubeturbo”) and namespace/project (example “turbo”):
   `kubectl edit deployment kubeturbo -n turbo`
1. Modify either the `image:` or `imagePullPolicy:`.  Use image.pullPolicy of “Always” if the image location and tag have not changed, to force the newer image to be pulled. Default value is “IfNotPresent”.
1. Once editted, the kubeturbo pod should redeploy
1. Repeat for every kubernetes / OpenShift cluster with a kubeturbo pod



There's no place like home... go back to the [Turbonomic Overview](https://github.com/turbonomic/kubeturbo/tree/master/README.md) or the [Deploy Overview](https://github.com/turbonomic/kubeturbo/tree/master/deploy/README.md).