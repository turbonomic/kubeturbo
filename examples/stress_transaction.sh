if [[ "$1" != "" ]]; then
    DIR="$1"
else
    echo "Please specify service address"
fi
echo $DIR
while true 
do
	#statements
	curl -X GET http://$DIR/index.php?cmd=set&key=messages&value=,wef
	sleep 0.01
done