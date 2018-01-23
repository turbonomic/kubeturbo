# redhat-img
Docker file to generate kubeturbo container image according to redhat's requirement

# build image
As it will install pkgs from RedHat repos, it is necessary to build this image from a RHEL machine which has registered the subscription from RedHat.

```console
docker build -t <imageName> .
```

# push image to redhat registry
Login redhat registry, and push the image:

```bash
docker login -u unused -e none registry.rhc4tp.openshift.com:443
docker tag $<imageName> registry.rhc4tp.openshift.com:443/$accountId/<imageName>
docker push registry.rhc4tp.openshift.com:443/$accountId/<imageName>
```

# push image to docker hub

Login docker hub first:
```console
  docker login docker.io
```

Then build the image, and push it into docker hub.
```bash
docker tag $<imageName> docker.io/vmturbo/kubeturbo:<tag>
# or build it:
# docker build -t docker.io/vmturbo/kubeturbo:<tag>
docker push docker.io/vmturbo/kubeturbo:<tag>
```


# Reference
[Redhat template](https://github.com/RHsyseng/container-rhel-examples/blob/master/starter/Dockerfile)
