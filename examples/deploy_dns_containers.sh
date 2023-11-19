#!/usr/bin/env bash

docker run -d --net=host --restart=always          \
                gcr.io/google_containers/kube2sky:1.11           \
                -v=10 -logtostderr=true -domain=kubernetes.local \
                -etcd-server="http://127.0.0.1:4001" |
docker run -d --net=host --restart=always         \
                -e ETCD_MACHINES="http://127.0.0.1:4001"        \
                -e SKYDNS_DOMAIN="kubernetes.local"             \
                -e SKYDNS_ADDR="0.0.0.0:53"                     \
                -e SKYDNS_NAMESERVERS="8.8.8.8:53,8.8.4.4:53"   \
                gcr.io/google_containers/skydns:2015-03-11-001
