#!/bin/bash
set -e -x

trap "exit 1" SIGTERM SIGINT

while curl http://localhost:8111 >/dev/null 2>&1; do
    # This is an upgrade hack from going from v0.7.5 to something newer
    echo Waiting for old ipsec container to stop
    sleep 2
done

export CHARON_PID_FILE=/var/run/charon.pid
rm -f ${CHARON_PID_FILE}

export PIDFILE=/var/run/rancher-net.pid
GCM=false

for ((i=0; i<6; i++)); do
    if ip xfrm state add src 1.1.1.1 dst 1.1.1.1 spi 42 proto esp mode tunnel aead "rfc4106(gcm(aes))" 0x0000000000000000000000000000000000000001 128 sel src 1.1.1.1 dst 1.1.1.1; then
        GCM=true
        ip xfrm state del src 1.1.1.1 dst 1.1.1.1 spi 42 proto esp 2>/dev/null || true
        break
    fi
    ip xfrm state del src 1.1.1.1 dst 1.1.1.1 spi 42 proto esp 2>/dev/null || true
    sleep 1
done

if [ "${RANCHER_DEBUG}" == "true" ]; then
    DEBUG="--debug"
else
    DEBUG=""
fi

export LOCAL_IP=$(ip route get 8.8.8.8 | grep via | awk '{print $7}')
export LOCAL_IP_WITH_SUBNET=$(ip addr | grep ${LOCAL_IP} | awk '{print $2}')

mkdir -p /etc/ipsec
curl -f -u ${CATTLE_ACCESS_KEY}:${CATTLE_SECRET_KEY} ${CATTLE_URL}/configcontent/psk > /etc/ipsec/psk.txt
curl -f -X PUT -d "" -u ${CATTLE_ACCESS_KEY}:${CATTLE_SECRET_KEY} ${CATTLE_URL}/configcontent/psk?version=latest
GATEWAY=$(ip route get 8.8.8.8 | awk '{print $3}')
iptables -t nat -I POSTROUTING -o eth0 -s $GATEWAY -j MASQUERADE
exec rancher-net \
-i ${LOCAL_IP_WITH_SUBNET} \
--pid-file ${PIDFILE} \
--gcm=$GCM \
--use-metadata \
--charon-launch \
--ipsec-config /etc/ipsec \
${DEBUG}
