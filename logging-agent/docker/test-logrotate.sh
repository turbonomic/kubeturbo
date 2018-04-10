#!/bin/sh

set -e

# kill -- -$$ sends a SIGTERM to the whole process group, thus killing also descendants.
trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT

# The directory for kubeturbo logs
export LOG_DIR=$PWD/var/log
mkdir -p $LOG_DIR

# Number of log copies to keep
export LOG_COPIES=10

# The log rotation period
export LOG_ROTATION_PERIOD=5

# Start log ratoation process
./logrotate.sh &

# Simulate logging process
while true; do
  echo "INFO: $(date)" >> $LOG_DIR/test.log.INFO.1
  echo "WARNING: $(date)" >> $LOG_DIR/test.log.WARNING.1
  echo "ERROR: $(date)" >> $LOG_DIR/test.log.ERROR.1
  sleep 1
done

