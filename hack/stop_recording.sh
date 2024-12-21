#!/bin/env bash
set -o errexit
set -o pipefail


LOCAL_PORT=9876
NS=turbo

function delete_wiremock {
  kubectl delete deploy wiremock -n ${NS}
  kubectl delete svc wiremock -n ${NS}
}

function stop_recording {

kubectl port-forward -n ${NS} service/wiremock ${LOCAL_PORT}:8080 >/dev/null 2>&1 &
pid=$!
# kill the port-forward regardless of how this script exits
trap '{
    kill $pid
}' EXIT

sleep 5
curl -X POST  http://localhost:${LOCAL_PORT}/__admin/recordings/stop

}

function copy_out_recording_data {
  PODNAME=$(kubectl get pods -n ${NS}|grep wiremock |awk '{ print $1 }' || true)
  kubectl exec  -n ${NS} ${PODNAME} --  bash -c "cd /home/wiremock && tar zcf recording_data.tgz mappings __files"
  kubectl cp -n ${NS}  ${PODNAME}:/home/wiremock/recording_data.tgz ./recording_data.tgz
  echo "The recording data is copied to $PWD/recording_data.tgz"
}


function main {
	if [ -n "$TURBO_NS" ]; then
		NS=${TURBO_NS}
	fi
	echo ""
	echo "*****************************************************************"
	echo "Stop WireMock recording"
	echo "*****************************************************************"
	echo ""
	stop_recording

  echo ""
	echo "*****************************************************************"
	echo "Copy out the recording data to the local"
	echo "*****************************************************************"
	echo ""
	copy_out_recording_data

	echo ""
	echo "*****************************************************************"
	echo "Delete WireMock deployment"
	echo "*****************************************************************"
	echo ""
	delete_wiremock
}

main
