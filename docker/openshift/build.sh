#!/bin/bash

kubeturbo=../../_output/kubeturbo.linux
img=vmturbo/kubeturbo:os35

cp $kubeturbo ./

docker build -t $img .
