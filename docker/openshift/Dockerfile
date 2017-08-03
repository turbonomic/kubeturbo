FROM alpine:3.3

RUN apk --update upgrade && apk add ca-certificates && update-ca-certificates
COPY ./kubeturbo.linux /bin/kubeturbo 
COPY ./run_kubeturbo.sh /bin/run_kubeturbo.sh

RUN chmod +x /bin/kubeturbo
RUN chmod +x /bin/run_kubeturbo.sh

ENTRYPOINT ["/bin/run_kubeturbo.sh"]
