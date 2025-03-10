#!/usr/bin/env bash

TURBO_PID=`/bin/ps -fu $USER| grep "kubeturbo " | grep -v "grep" | awk '{print $2}'`
echo $TURBO_PID
kill $TURBO_PID
