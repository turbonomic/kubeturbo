> NOTE: The user performing the steps to create a namespace, service account, and `cluster-admin` clusterrolebinding, will need to have cluster-admin role.

Deploy the kubeturbo pod with the following resources:

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
  apiGroup: rbac.authorization.k8s.io  
```

3. create a configMap for kubeturbo, The <TURBONOMIC_SERVER_VERSION> is Turbonomic release version, e.g. 5.9.0 or 6.0.0
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
        kubeturbo.io/monitored: "false"
      labels:
        app: kubeturbo
    spec:
      serviceAccount: turbo-user
      containers:
        - name: kubeturbo
          # Replace the image with desired version
          image: vmturbo/kubeturbo:redhat-6.1dev
          imagePullPolicy: IfNotPresent
          args:
            - --turboconfig=/etc/kubeturbo/turbo.config
            - --v=2
            # Uncomment the following two args if running in Openshift
            #- --kubelet-https=true
            #- --kubelet-port=10250
            # Uncomment the following arg if using IP for stitching
            #- --stitch-uuid=false
          volumeMounts:
          - name: turbo-config
            mountPath: /etc/kubeturbo
            readOnly: true
      volumes:
      - name: turbo-config
        configMap:
          name: turbo-config
      restartPolicy: Always
```
Note: If Kubernetes version is older than 1.6, then add another arg for move/resize action `--k8sVersion=1.5`
