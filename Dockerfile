FROM ubuntu:14.04
MAINTAINER leodotcloud@gmail.com

RUN apt-get update && \
    apt-get install -y vim tcpdump ethtool && \
    cp /usr/sbin/tcpdump /root/tcpdump

RUN mkdir -p /opt/rancher/bin

ADD run.sh /opt/rancher/bin/run.sh
ADD rancher-net /opt/rancher/bin/rancher-net

CMD ["/opt/rancher/bin/run.sh"]
