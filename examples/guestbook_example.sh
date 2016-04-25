./cluster/kubectl.sh create -f examples/guestbook/redis-master-controller.yaml | \
./cluster/kubectl.sh create -f examples/guestbook/redis-master-service.yaml | \
./cluster/kubectl.sh create -f examples/guestbook/redis-slave-controller.yaml | \
./cluster/kubectl.sh create -f examples/guestbook/redis-slave-service.yaml | \
./cluster/kubectl.sh create -f examples/guestbook/frontend-controller.yaml | \
./cluster/kubectl.sh create -f examples/guestbook/frontend-service.yaml 