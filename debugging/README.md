## Preparing a Kubeturbo Image For Debugging

To debug Kubeturbo, it must be wrapped by [delve](https://github.com/go-delve/delve), a golang
debugging monitor.  This process is automated by building the debug target:

    make debug
    
This will create a Docker image named `turbonomic/kubeturbo:6.4debug`.  Specify this image in the
`spec.containers.image` field in your Kubernetes Kubeturbo deployment descriptor.  After
deployment, the debugger will be listening on port 40000.  A simple way to make this port accessible
to your debugger is to forward the port via `kubectl port-forward`:

    kubectl port-forward {kubeturbo-pod-name} 40000:40000
