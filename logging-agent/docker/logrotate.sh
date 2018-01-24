#!/bin/sh

# The directory for kubeturbo logs. Default is /var/log.
LOG_DIR="${LOG_DIR:-/var/log}"

# Number of log copies to keep. Default is 30 days.
LOG_COPIES="${LOG_COPIES:-30}"

# The log rotation period. Default is 24 hours.
LOG_ROTATION_PERIOD="${LOG_ROTATION_PERIOD:-86400}"

echo "Performing periodic log rotatation with parameters: LOG_DIR: $LOG_DIR, \
LOG_COPIES: $LOG_COPIES, LOG_ROTATION_PERIOD: $LOG_ROTATION_PERIOD seconds"



# The pattern of kubeturbo log files
# The log filenames created by glog will end up with the associated pid in the format:
# "<program name>.<hostname>.<user name>.log.<severity level>.<date>.<time>.<pid>"
# (e.g., "hello_world.example.com.hamaji.log.INFO.20080709-222411.10474")
# So, the filenames end up with a digit while the rotated files end up with .log as defined below
# so that the rotation will not include those rotated files
LOG_FILES=$LOG_DIR/*.log.[INFO,ERROR,WARNING]*[0-9]

# Keep running log rotation with period of $PERIOD seconds
# Compress the rotated files except for the latest one (i.e., delay compression)
while true; do
  sleep $LOG_ROTATION_PERIOD

  for file in $LOG_FILES; do
    # Check if the file exists
    [ -e "$file" ] || continue

    echo "perform log rotating for file $file"
    i=$LOG_COPIES
    while [ "$i" != "1" ]; do
      if [ -f $file.`expr $i - 1`.log.gz ]; then
        mv -f $file.`expr $i - 1`.log.gz $file.$i.log.gz
      fi

      # Move $file.1.log to $file.2.log and compress it
      if [ "$i" == "2" ] && [ -f $file.1.log ]; then
        mv -f $file.1.log $file.2.log
        gzip $file.2.log
      fi

      i=`expr $i - 1`
    done
    cp -p $file $file.1.log
    >$file
  done

  echo "Finished log rotation at $(date)"
done
