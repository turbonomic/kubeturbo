#!/bin/bash

kubeturbo=../../_output/kubeturbo.linux
img=vmturbo/kubeturbo:os34

cp $kubeturbo ./

docker build -t $img .
