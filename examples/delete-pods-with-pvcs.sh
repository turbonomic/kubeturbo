#!/bin/bash

# Delete multiple Pods with PVCs  
NAME=$1

if [ -z $NAME ]; then
  echo "Usage: $0 <name>. Pod name should not be empty. Exiting." && exit 1
fi

COMMAND=oc

for it in $($COMMAND get deploy,pvc -o name | grep $NAME); do $COMMAND delete ${it}; done
