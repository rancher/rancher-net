FROM ubuntu:16.04
RUN apt-get update && apt-get install -y \
    iproute2 \
    iptables \
    psmisc \
    tcpdump \
    vim-tiny \
    curl \
    openssl
ADD strongswan.tar.gz /
RUN mkdir -p /opt/cni/bin /etc/cni/net.d
ENV CNI v0.3.0-rancher3
RUN curl -sfSL https://github.com/rancher/cni/releases/download/${CNI}/cni-${CNI}.tar.gz | \
    tar xvzf - -C /tmp ./bridge ./loopback && \
    mv /tmp/bridge /usr/bin/rancher-bridge && \
    mv /tmp/loopback /usr/bin/loopback

RUN curl -sfSL https://github.com/rancher/rancher-cni-ipam/releases/download/v0.0.1/rancher-cni-ipam.tar.gz | \
    tar xvzf - -C /usr/bin

COPY start.sh rancher-net /usr/bin/
CMD ["start.sh"]
