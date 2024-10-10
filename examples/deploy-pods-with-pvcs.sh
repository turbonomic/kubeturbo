#!/bin/bash

# Create multiple Pods with PVCs. Used it for a ROSA cluster.  
NAME=$1

if [ -z $NAME ]; then
  echo "Usage: $0 <name> <count>. Pod name should not be empty. Exiting." && exit 1
fi

COUNT=$2
if [ -z $COUNT ]; then
  echo "Usage: $0 <name> <count>. Pod count should not be empty. Exiting" && exit 1
fi

COMMAND=oc

for i in $(seq $COUNT); do
  cat <<-EOF | $COMMAND create -f -
    kind: PersistentVolumeClaim
    apiVersion: v1
    metadata:
      name: ${NAME}-$i
    spec:
      accessModes:
        - ReadWriteOnce
      resources:
        requests:
          storage: 1Mi
EOF

  cat <<-EOF | $COMMAND create -f -
    apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: ${NAME}-$i
    spec:
      replicas: 1
      selector:
        matchLabels:
          app: mysql
      template:
        metadata:
          labels:
            app: mysql
        spec:
          volumes:
          - name: pvc-volume
            persistentVolumeClaim:
              claimName: ${NAME}-$i
          containers:
          - image: registry.redhat.io/rhscl/mysql-56-rhel7
            name: mysql
            env:
              # Use secret in real usage
            - name: MYSQL_ROOT_PASSWORD
              value: password
            ports:
            - containerPort: 3306
              name: mysql
            volumeMounts:
            - name: pvc-volume
              mountPath: /var/lib/mysql
EOF
done
