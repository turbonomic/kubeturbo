### Logging Agent
The logging agent is a sidecar container for `kubeturbo` to perform rotation and compression of log files.

The logging agent by default performs daily rotation and compression to the `kubeturbo` log files located in `/var/log`. The files will be kept for 30 days before cleaned up.

### Deploy `kubeturbo` with the sidecar container

To deploy `kubeturbo` with the sidecar container, add the container of logging agent in the `kubeturbo` yaml file as in the following example. In this yaml file, there is one additional volume `varlog`. `varlog` is shared between `kubeturbo` and `sidecar` containers and is mounted to `/var/log` as shown in their container config (see `volumeMounts`).

```yaml
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: kubeturbo
  labels:
    app: kubeturbo
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: kubeturbo
    spec:
      containers:
        - name: kubeturbo
          # Replace the image with desired version
          image: vmturbo/kubeturbo:redhat-6.1dev
          imagePullPolicy: IfNotPresent
          args:
            - --turboconfig=/etc/kubeturbo/turbo.config
            - --v=3
            # Uncomment the following two args if running in Openshift
            #- --kubelet-https=true
            #- --kubelet-port=10250
          volumeMounts:
          - name: turbo-config
            mountPath: /etc/kubeturbo
            readOnly: true
          - name: varlog
            mountPath: /var/log
        - name: logging-agent
          image: vmturbo/logging-agent:redhat-6.1dev
          imagePullPolicy: IfNotPresent
          volumeMounts:
          - name: varlog
            mountPath: /var/log
      volumes:
      - name: turbo-config
        configMap:
          name: turbo-config
      - name: varlog
        emptyDir: {}
      restartPolicy: Always
```

### Retrieve log files

Use the following `kubectl` command to retrieve the log files created by `kubeturbo`

```console
$kubectl cp <POD_NAME>:/var/log -c kubeturbo <LOCAL_DIR>
```

