#!/bin/sh

kubebin=/bin/kubeturbo

logv=3
k8sVersion=1.5.0
kubelet_https=true
kubelet_port=10250
usevmware=false
alsologtostderr=true

master=""
kubeconfig=""
turboconfig=""
none_scheduler=""

for i in "$@"
do
case $i in
    -v=*|--v=*)
    logv="${i#*=}"
    shift # past argument=value
    ;;
    --alsologtostderr*)
    alsologtostderr="${i#*=}"
    shift # past argument=value
    ;;
    --master=*)
    master="${i#*=}"
    shift # past argument=value
    ;;
    --kubeconfig=*)
    kubeconfig="${i#*=}"
    shift # past argument=value
    ;;
    --turboconfig=*)
    turboconfig="${i#*=}"
    shift # past argument=value
    ;;
    --k8sVersion=*)
    k8sVersion="${i#*=}"
    shift # past argument=value
    ;;
    --kubelet-port=*)
    kubelet_port="${i#*=}"
    shift # past argument=value
    ;;
    --kubelet-https=*)
    kubelet_https="${i#*=}"
    shift # past argument=value
    ;;
    --usevmware=*)
    usevmware="${i#*=}"
    shift # past argument=value
    ;;
    --noneSchedulerName=*)
    none_scheduler="${i#*=}"
    shift # past argument=value
    ;;
    -h|--help)
    $kubebin --help
    exit 0
    ;;
    *)
            # unknown option
    ;;
esac
done

opts="--alsologtostderr=$alsologtostderr"
opts="$opts --v=$logv"
opts="$opts --kubelet-https=$kubelet_https"
opts="$opts --kubelet-port=$kubelet_port"
opts="$opts --usevmware=$usevmware"
opts="$opts --k8sVersion=$k8sVersion"

if [ -n "$master" ] ; then
    opts="$opts --master=$master"
fi

if [ -n "$kubeconfig" ] ; then
    opts="$opts --kubeconfig=$kubeconfig"
fi

if [ -n "$turboconfig" ] ; then
    opts="$opts --turboconfig=$turboconfig"
fi

if [ -n "$none_scheduler" ] ; then
   opts="$opts --noneSchedulerName=$none_scheduler"
fi

cmd="$kubebin $opts"
echo "$cmd"

$cmd
