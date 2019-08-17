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

Assign `cluster-admin` role with the following cluster role binding:
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
Note: This cluster-admin role can be view only, which will allow for metrics but will not allow for actions to execute.  For examples of a customized admin role narrowed to minimum resources and verbs, see the sample [turbo-admin.yaml](https://github.com/turbonomic/kubeturbo/blob/master/deploy/kubeturbo_yamls/turbo-admin.yaml). For a minimal admin with read only see [turbo-reader.yaml](https://github.com/turbonomic/kubeturbo/blob/master/deploy/kubeturbo_yamls/turbo-reader.yaml) 

**3.** Create a configMap for kubeturbo. Strongly advise to use a sample provided which is located [here](https://github.com/turbonomic/kubeturbo/blob/master/deploy/kubeturbo_yamls/turbo_configMap_63sample.yml).  You will need to provide your Turbo Server details here.  Running CWOM? Refer to [CWOM -> Turbonomic Server -> kubeturbo version](https://github.com/turbonomic/kubeturbo/tree/master/deploy/version_mapping_kubeturbo_Turbo_CWOM.md) for the mapping of CWOM to Turbo versions. 
The ConfigMap serves two functions, depending on the kubeturbo image being used.
1.	Defines how to connect to the Turbonomic Server. The Turbonomic Server instance details are defined under “communicationConfig”, and optionally what the cluster is identified as in the Turbo UI under “targetConfig”.
2.	How to identify master nodes and daemon pods. A new feature introduced in Turbonomic 6.3, is the ability to consolidate clusters for efficiency.  We provide you a way to designate how your master nodes are identified, since we want to prevent suspending masters that are for HA.  The user can also determine how daemon pods are identified, since we do not want to consider them; nodes only running these pods would be candidates for suspension.

The following chart will help you populate the required values for your configMap.

Property|Purpose|Required|Default Value|Accepted Values
------------ | ------------- | ------------- | ------------- | -------------
serverMeta.version|Turbo Server version|yes - all versions|none|x.x.x. After 6.3+, only the first version.major is required
serverMeta.turboServer|Server URL|yes - all versions|none| https://{yourServerIPAddressOrFQN}
restAPIConfig.opsManagerUserName|admin role user to log into Turbo|yes - all versions|none|same value as provided for login screen (see Note)
restAPIConfig.opsManagerPassword|password to log into Turbo|yes - all versions|none|same value as provided for login screen
targetConfig.targetName|uniquely identifies k8s clusters|no - all versions|"Name_Your_Cluster"|string, upper lower case, limited special characters "-" or "_"
masterNodeDetectors.nodeNamePatterns|identifies master nodes by node name|in 6.3+|name includes `.*master.*`. If no match, this is ignored.| regex used, value in quotes `.*master.*`
masterNodeDetectors.nodeLabels|identifies master nodes by node label key value pair|in 6.3+, any value for label `node-role.kubernetes.io/master` If no match, this is ignored.|masters not uniquely identified|key value pair, regex used, values in quotes `{"key": "node-role.kubernetes.io/master", "value": ".*"}`
daemonPodDetectors.namespaces|identifies all pods in the namespace to be ignored for cluster consolidation|no - 6.3+|daemonSet kinds are by default allow node suspension. Adding this parameter changes default.| regex used, values in quotes & comma separated`"kube-system", "kube-service-catalog", "openshift-.*"`
daemonPodDetectors.podNamePatterns|identifies all pods matching this pattern to be ignored for cluster consolidation|no - 6.3+|daemonSet kinds are by default allow node suspension. Adding this parameter changes default.|regex used `".*ignorepod.*"`

(*) UserName Note: If your Turbonomic Server is configured to manage users via AD, the <Turbo_username> value can be either a local or AD user.  For AD user, the format will be “<domain>//<username>” – both “/” are required.

To manage the way master nodes are identified, your configMap will include the `"masterNodeDetectors"` section after `"targetConfig"`.  You can use either or both values. NOTE: If you want to leave the default behavior of master nodes not identified, or you are running a hosted kubernetes service where the master is not managed by you (AKS, EKS, GKE, etc) do not include this section.  
```yaml
        },
        "masterNodeDetectors": {
           "nodeNamePatterns": [ ".*master.*" ],
           "nodeLabels": [ {"key": "node-role.kubernetes.io/master", "value": ".*"} ]
        },
```
Daemon pods are by default by resource kind of daemonSet. These pods will be ignored for node scale in. If you want to change the way daemon pods are identified, your configMap will include this section after `"targetConfig"`.  You can use either value or both.  NOTE: It is recommended that you do not include this section.
```yaml
        },
        "daemonPodDetectors": {
           "namespaces": ["kube-system", "kube-service-catalog"],
           "podNamePatterns": [".*ignorepod.*"]
        },
```


**4.** Create a deployment for kubeturbo.  The image tag used will depend somewhat on your Turbo Server version.  For Server versions of 6.1.x - 6.2.x, use tag "6.2".  For Server versions of 6.3.1+, use "6.3".  Running CWOM? Go here to see conversion chart for [CWOM -> Turbonomic Server -> kubeturbo version](https://github.com/turbonomic/kubeturbo/tree/master/deploy/version_mapping_kubeturbo_Turbo_CWOM.md). 
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
          image: turbonomic/kubeturbo:6.4.0
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
NOTE: Starting with Turbonomic 6.3+, you do not need to make this configMap modification if updating to a minor version like 6.3.0 -> 6.3.1, which will now be automatically handled.  You would only need to make this change if you are making a major change, going from 6.3.1 -> 6.4.0, or 6.3.1 -> 7.0.0.

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