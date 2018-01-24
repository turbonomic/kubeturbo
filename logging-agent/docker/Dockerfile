FROM registry.access.redhat.com/rhel-atomic:7.4-97

COPY logrotate.sh /

RUN chmod 755 /logrotate.sh

ENTRYPOINT /logrotate.sh
