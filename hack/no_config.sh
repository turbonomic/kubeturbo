#!/usr/bin/env bash

OUTPUT_DIR=${OUTPUT_DIR:-"_output"}

sudo -E "./${OUTPUT_DIR}/kubeturbo" \
	 --v=3 \
	 --master="http://172.17.0.1:8080" \
	 --etcd-servers="http://127.0.0.1:2379" \
	 --serveraddress="10.10.192.87" \
	 --serverport="8080" \
	 --opsmanagerusername="administrator" \
	 --opsmanagerpassword="a" > "/tmp/kubeturbo.log" 2>&1 &
