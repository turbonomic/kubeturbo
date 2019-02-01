> NOTE: The user performing the steps to create a namespace, service account, and `cluster-admin` clusterrolebinding, will need to have cluster-admin role.

The kubeturbo pod can be easily deployed using a helm chart

`helm install --name k8s-cluster-control kubeturbo --namespace turbonomic --set serverMeta.turboServer=https://1.2.3.4/ --set serverMeta.version=6.3 --set restAPIConfig.opsManagerUserName=user --set restAPIConfig.opsManagerPassword=password --set targetConfig.targetName=YourClusterName`

Or deploy the kubeturbo pod with the following resources:

1. create a namespace

Use an existing namespace, or create one where to deploy kubeturbo. The yaml examples will use `turbo`.

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: turbo 
```

2. Create a service account, and add the role of cluster-admin
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

3. create a configMap for kubeturbo, The <TURBONOMIC_SERVER_VERSION> is Turbonomic release version, e.g. 6.3.0 or 6.2.8.  To distinguish between different k8s clusters, supply a targetName value which will name the k8s cluster groups created in Turbonomic.
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


4. Create a deployment for kubeturbo
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
          image: vmturbo/kubeturbo:6.2
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
      volumes:
      - name: turbo-volume
        configMap:
          name: turbo-config
      restartPolicy: Always
```
Note: If Kubernetes version is older than 1.6, then add another arg for move/resize action `--k8sVersion=1.5`
