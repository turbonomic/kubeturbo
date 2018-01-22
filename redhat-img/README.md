# redhat-img
Docker file to generate kubeturbo container image according to redhat's requirement

# build it
As it will install pkgs from RedHat repos, it is necessary to build this image from a RHEL machine which has registered the subscription from RedHat.

```console
docker build -t <mytag> .
```

# test the image
```console
sh run.sh
```


# Reference
[Redhat template](https://github.com/RHsyseng/container-rhel-examples/blob/master/starter/Dockerfile)
