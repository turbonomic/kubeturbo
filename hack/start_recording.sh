#!/bin/env bash
set -o errexit
set -o pipefail

WIREMOCK_IMAGE_NAME=icr.io/cpopen/turbonomic/wiremock
WIREMOCK_IMAGE_TAG=3.3.1
LOCAL_PORT=9876
NS=turbo
SA=turbo-user

function create_wiremock {
	cat << EOF | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: wiremock
  namespace: ${NS}
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/instance: wiremock
      app.kubernetes.io/name: wiremock-kubeturbo
  template:
    metadata:
      annotations:
        kubeturbo.io/monitored: "false"
      labels:
        app.kubernetes.io/instance: wiremock
        app.kubernetes.io/name: wiremock-kubeturbo
    spec:
      # Update serviceAccount if needed
      serviceAccountName: ${SA}
      containers:
      - name: wiremock
        args:
        # Update this if you want to use a different port
        - --port=8080
        - --root-dir=/home/wiremock
        image: ${WIREMOCK_IMAGE_NAME}:${WIREMOCK_IMAGE_TAG}
        readinessProbe:
          failureThreshold: 10
          httpGet:
            path: /__admin
            # The port here needs to be same with the one defines in the args
            port: 8080
            scheme: HTTP
          periodSeconds: 20
          successThreshold: 1
          timeoutSeconds: 10
      restartPolicy: Always
---
apiVersion: v1
kind: Service
metadata:
  name: wiremock
  namespace: ${NS}
spec:
  ports:
  - port: 8080
    protocol: TCP
    # The port here needs to be same with the one defined in the args of the deployment
    targetPort: 8080
  selector:
    app.kubernetes.io/instance: wiremock
    app.kubernetes.io/name: wiremock-kubeturbo
  type: ClusterIP
EOF

echo "Waiting for the WireMock to be ready..."
PODEADY=$(kubectl get pods -n ${NS}|grep wiremock |awk '{ print $2 }' || true)
C=0
while [ "${PODEADY}" != "1/1" ]; do
    sleep 5
    C=$((C + 1))
    if [ $C -eq 20 ]; then
        echo ""
        echo "WireMock did not get ready"
        exit 1
    fi
    PODEADY=$(kubectl get pods -n ${NS} |grep wiremock |awk '{ print $2 }' || true)
done

}

function start_recording {

kubectl port-forward -n ${NS} service/wiremock ${LOCAL_PORT}:8080 >/dev/null 2>&1 &
pid=$!
# kill the port-forward regardless of how this script exits
trap '{
    kill $pid
}' EXIT

sleep 5
curl -X POST  http://localhost:${LOCAL_PORT}/__admin/recordings/start -H 'Content-Type: application/json' -H 'Accept: application/json' -d '{
"targetBaseUrl":"https://kubernetes.default.svc.cluster.local",
"extractBodyCriteria" : {
    "textSizeThreshold" : "0",
    "binarySizeThreshold" : "0"
  }
}'

echo ""
echo "*****************************************************************"
echo "Check WireMock Status"
echo "*****************************************************************"
echo ""
curl -X GET http://localhost:${LOCAL_PORT}/__admin/recordings/status

}


function main {
	if [ -n "$TURBO_NS" ]; then
		NS=${TURBO_NS}
	fi
	if [ -n "$TURBO_SA" ]; then
		SA=${TURBO_SA}
	fi
	echo ""
	echo "*****************************************************************"
	echo "Create WireMock deployment"
	echo "*****************************************************************"
	echo ""
	create_wiremock

	echo ""
	echo "*****************************************************************"
	echo "Start WireMock recording"
	echo "*****************************************************************"
	echo ""
	start_recording
}

main
