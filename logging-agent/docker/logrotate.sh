#!/bin/sh

# The directory for kubeturbo logs. Default is /var/log.
LOG_DIR="${LOG_DIR:-/var/log}"

# Number of log copies to keep. Default is 30.
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
INFO_LOG_FILES=$LOG_DIR/*.log.INFO*[0-9]
WARNING_LOG_FILES=$LOG_DIR/*.log.WARNING*[0-9]
ERROR_LOG_FILES=$LOG_DIR/*.log.ERROR*[0-9]

# Function to rotate the given log files in the parameter. Delay compression applies.
# E.g., for the log file: test.log and for LOG_COPIES=10,
# first rotation: test-<date>.1.log (no compression)
# second rotation: test-<date>.1.log and test-<date>.2.log.gz
# third rotation: test-<date>.1.log, test-<date>.2.log.gz, and test-<date>.3.log.gz
# ...
# After the 10th rotation: test-<date>.1.log, test-<date>.2.log.gz, ..., and test-<date>.10.log.gz (max 10 copies)
rotate () {
  logfiles=$1

  for file in $logfiles; do
    # Check if the file exists
    [ -e "$file" ] || continue

    echo "perform log rotating for file $file"
    i=$LOG_COPIES

    # Move all the outdated log files to the temp files, which
    # will only keep the last outdated files
    for gzfile in $logfiles*.$i.log.gz $logfiles*.$i.log; do
      if [ -f "$gzfile" ]; then
        newfile=$file.tmp
        mv -f $gzfile $newfile
      fi
    done

    while [ "$i" != "1" ]; do
      # Move the .log.gz files to next index (e.g. file.<date>.3.log.gz to file.<date>.4.log.gz)
      j=`expr $i - 1`
      for gzfile in $logfiles*.$j.log.gz; do
        if [ -f "$gzfile" ]; then
          newfile=$(dirname $gzfile)/$(basename $gzfile .$j.log.gz).$i.log.gz
          mv -f $gzfile $newfile
        fi
      done

      # Move .1.log files to .2.log files and perform compression (to .2.log.gz)
      if [ "$i" == "2" ]; then
        for gzfile in $logfiles*.1.log; do
          if [ -f "$gzfile" ]; then
            newfile=$(dirname $gzfile)/$(basename $gzfile .1.log).2.log
            mv -f $gzfile $newfile
            gzip $newfile
          fi
        done
      fi

      i=`expr $i - 1`
    done
    # Copy the log file to the rotated file <date>.1.log (e.g., file.log -> file.<date>.1.log)
    cp -p $file $file-$(date "+%Y%m%d-%H%M%S").1.log
    >$file
  done
}

# Keep running log rotation with period of $PERIOD seconds
# Compress the rotated files except for the latest one (i.e., delay compression)
while true; do
  sleep $LOG_ROTATION_PERIOD

  # Start rotating
  rotate $INFO_LOG_FILES
  rotate $WARNING_LOG_FILES
  rotate $ERROR_LOG_FILES

  echo "Finished log rotation at $(date)"
done

