DNS_CONTAINER=`/usr/bin/docker ps | grep "sky" | grep -v "grep" | awk '{print $1}'`
echo $DNS_CONTAINER
docker stop $DNS_CONTAINER
docker rm $DNS_CONTAINER