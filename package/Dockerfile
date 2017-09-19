FROM rancher/agent-base:v0.3.0

ENV CNI v0.3.0

ADD rancher-vxlan.tar.gz /
ADD rancher-ipsec.tar.gz /
ADD rancher-cni-bridge.tar.gz /opt/cni/bin
ADD rancher-cni-ipam.tar.gz /opt/cni/bin
ADD rancher-cni-driver.tar.gz /

RUN mkdir -p /etc/cni/net.d \
    && curl -sfSL https://github.com/containernetworking/cni/releases/download/${CNI}/cni-${CNI}.tgz | tar xvzf - -C /tmp ./loopback \
    && mv /tmp/* /opt/cni/bin

CMD ["sleep", "infinity"]
